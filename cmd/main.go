package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/clastix/talos-csr-signer/proto"
)

const (
	DefaultPort = "50001"
)

type server struct {
	pb.UnimplementedSecurityServiceServer
	caCert       []byte
	caPrivateKey interface{}
	validToken   string
}

// Certificate implements the SecurityService.Certificate RPC
func (s *server) Certificate(ctx context.Context, req *pb.CertificateRequest) (*pb.CertificateResponse, error) {
	log.Printf("=== New Certificate Request Received ===")

	// Extract and validate token from metadata
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		log.Printf("ERROR: No metadata in request")
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}
	log.Printf("Metadata extracted successfully")

	// Talos sends token directly in metadata "token" field, not as authorization header
	tokenHeader := md.Get("token")
	if len(tokenHeader) == 0 {
		log.Printf("ERROR: No token in metadata")
		log.Printf("Available metadata keys: %v", md)
		return nil, status.Error(codes.Unauthenticated, "missing token")
	}
	log.Printf("Token found in metadata")

	token := tokenHeader[0]
	log.Printf("Token prefix: %s...", token[:min(8, len(token))])

	if token != s.validToken {
		log.Printf("ERROR: Invalid token received")
		log.Printf("  Received: %s...", token[:min(8, len(token))])
		log.Printf("  Expected: %s...", s.validToken[:min(8, len(s.validToken))])
		return nil, status.Error(codes.Unauthenticated, "invalid token")
	}
	log.Printf("Token validated successfully")

	// Parse the CSR
	log.Printf("Parsing CSR (length: %d bytes)", len(req.Csr))
	block, _ := pem.Decode(req.Csr)
	if block == nil {
		log.Printf("ERROR: Failed to decode PEM CSR")
		return nil, status.Error(codes.InvalidArgument, "failed to decode PEM CSR")
	}
	log.Printf("CSR PEM decoded successfully")

	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		log.Printf("ERROR: Failed to parse CSR: %v", err)
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("failed to parse CSR: %v", err))
	}
	log.Printf("CSR parsed successfully")

	// Verify CSR signature
	if err := csr.CheckSignature(); err != nil {
		log.Printf("ERROR: Invalid CSR signature: %v", err)
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("invalid CSR signature: %v", err))
	}
	log.Printf("CSR signature verified")

	log.Printf("CSR Details: Subject=%s, DNSNames=%v, IPAddresses=%v",
		csr.Subject.CommonName, csr.DNSNames, csr.IPAddresses)

	// Parse CA certificate
	caBlock, _ := pem.Decode(s.caCert)
	if caBlock == nil {
		return nil, status.Error(codes.Internal, "failed to decode CA certificate")
	}

	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to parse CA cert: %v", err))
	}

	// Create certificate template
	serialNumber, err := generateSerialNumber()
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to generate serial: %v", err))
	}

	template := &x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               csr.Subject,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour), // 1 year validity
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              csr.DNSNames,
		IPAddresses:           csr.IPAddresses,
	}

	// Sign the certificate
	certDER, err := x509.CreateCertificate(nil, template, caCert, csr.PublicKey, s.caPrivateKey)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to create certificate: %v", err))
	}

	// Encode signed certificate to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	log.Printf("✓ Certificate signed successfully for: %s (valid until: %s)",
		csr.Subject.CommonName, template.NotAfter.Format(time.RFC3339))
	log.Printf("=== Certificate Request Completed Successfully ===")

	return &pb.CertificateResponse{
		Ca:  s.caCert,
		Crt: certPEM,
	}, nil
}

func extractToken(authHeader string) string {
	// Format: "Basic <base64(token:)>"
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Basic" {
		return ""
	}

	// Decode base64
	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}

	// Token format is "token:" - extract token part
	tokenParts := strings.SplitN(string(decoded), ":", 2)
	return tokenParts[0]
}

func generateSerialNumber() (*big.Int, error) {
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, serialNumberLimit)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// generateServerCert generates a TLS certificate for the gRPC server, signed by the Talos Machine CA
func generateServerCert(caCertPEM []byte, caPrivateKey interface{}, serverIPs []string) (tls.Certificate, error) {
	// Generate private key
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Create certificate template
	serialNumber, err := generateSerialNumber()
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to generate serial number: %w", err)
	}

	// Parse IP addresses
	ipAddresses := []net.IP{
		net.ParseIP("127.0.0.1"), // Localhost
	}
	for _, ipStr := range serverIPs {
		if ip := net.ParseIP(ipStr); ip != nil {
			ipAddresses = append(ipAddresses, ip)
		}
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  ipAddresses,
		DNSNames: []string{
			"talos-csr-signer",
			"talos-csr-signer.default",
			"talos-csr-signer.default.svc",
			"talos-csr-signer.default.svc.cluster.local",
		},
	}

	// Parse CA certificate
	caBlock, _ := pem.Decode(caCertPEM)
	if caBlock == nil {
		return tls.Certificate{}, fmt.Errorf("failed to decode CA certificate")
	}

	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	// Sign server certificate with Talos Machine CA (not self-signed!)
	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &priv.PublicKey, caPrivateKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to create certificate: %w", err)
	}

	// Encode server certificate to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	// Append CA certificate to create complete chain
	// This is critical: clients need the full chain to verify
	certChainPEM := append(certPEM, caCertPEM...)

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to marshal private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	// Load as TLS certificate with full chain (server cert + CA cert)
	cert, err := tls.X509KeyPair(certChainPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to load key pair: %w", err)
	}

	return cert, nil
}

func main() {
	// Read configuration from environment
	port := os.Getenv("PORT")
	if port == "" {
		port = DefaultPort
	}

	caCertPath := os.Getenv("CA_CERT_PATH")
	if caCertPath == "" {
		caCertPath = "/etc/talos-csr-signer/ca.crt"
	}

	caKeyPath := os.Getenv("CA_KEY_PATH")
	if caKeyPath == "" {
		caKeyPath = "/etc/talos-csr-signer/ca.key"
	}

	token := os.Getenv("TALOS_TOKEN")
	if token == "" {
		log.Fatal("TALOS_TOKEN environment variable is required")
	}

	// Read server IPs for TLS certificate (optional)
	serverIPsStr := os.Getenv("SERVER_IPS")
	var serverIPs []string
	if serverIPsStr != "" {
		serverIPs = strings.Split(serverIPsStr, ",")
	}

	// Load CA certificate
	caCertPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		log.Fatalf("Failed to read CA certificate: %v", err)
	}

	// Load CA private key
	caKeyPEM, err := os.ReadFile(caKeyPath)
	if err != nil {
		log.Fatalf("Failed to read CA private key: %v", err)
	}

	// Parse CA private key
	block, _ := pem.Decode(caKeyPEM)
	if block == nil {
		log.Fatal("Failed to decode PEM private key")
	}

	var caPrivateKey interface{}
	switch block.Type {
	case "ED25519 PRIVATE KEY":
		caPrivateKey, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		caPrivateKey, err = x509.ParseECPrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		caPrivateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		caPrivateKey, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	default:
		log.Fatalf("Unsupported private key type: %s", block.Type)
	}

	if err != nil {
		log.Fatalf("Failed to parse CA private key: %v", err)
	}

	log.Printf("Loaded CA certificate and private key")
	log.Printf("Valid token prefix: %s...", token[:min(8, len(token))])

	// Generate TLS certificate for the gRPC server, signed by Talos Machine CA
	cert, err := generateServerCert(caCertPEM, caPrivateKey, serverIPs)
	if err != nil {
		log.Fatalf("Failed to generate TLS certificate: %v", err)
	}
	if len(serverIPs) > 0 {
		log.Printf("Generated TLS certificate for gRPC server with IPs: %v", append(serverIPs, "127.0.0.1"))
	} else {
		log.Printf("Generated TLS certificate for gRPC server (using DNS names only)")
	}

	// Create TLS credentials
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.NoClientCert, // Don't require client certificates
	}
	creds := credentials.NewTLS(tlsConfig)

	// Create gRPC server with TLS
	s := &server{
		caCert:       caCertPEM,
		caPrivateKey: caPrivateKey,
		validToken:   token,
	}

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("Failed to listen on port %s: %v", port, err)
	}

	grpcServer := grpc.NewServer(grpc.Creds(creds))
	pb.RegisterSecurityServiceServer(grpcServer, s)

	log.Printf("Talos CSR Signer listening on port %s with TLS enabled", port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
