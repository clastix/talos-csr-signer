// Copyright 2025 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

// Package server is the gRPC implementation for the Talos SecurityServiceServer interface signature.
package server

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/clastix/talos-csr-signer/pkg/proto"
)

// Server is the struct satisfying the SecurityServiceServer interface.
type Server struct {
	pb.UnimplementedSecurityServiceServer
	CACert       []byte
	CAPrivateKey interface{}
	ValidToken   string
}

// Certificate implements the SecurityService.Certificate RPC.
//
//nolint:wrapcheck
func (s *Server) Certificate(ctx context.Context, req *pb.CertificateRequest) (*pb.CertificateResponse, error) {
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

	if token != s.ValidToken {
		log.Printf("ERROR: Invalid token received")
		log.Printf("  Received: %s...", token[:min(8, len(token))])
		log.Printf("  Expected: %s...", s.ValidToken[:min(8, len(s.ValidToken))])

		return nil, status.Error(codes.Unauthenticated, "invalid token")
	}

	log.Printf("Token validated successfully")

	// Parse the CSR
	log.Printf("Parsing CSR (length: %d bytes)", len(req.GetCsr()))

	block, _ := pem.Decode(req.GetCsr())
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
	caBlock, _ := pem.Decode(s.CACert)
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
	certDER, err := x509.CreateCertificate(nil, template, caCert, csr.PublicKey, s.CAPrivateKey)
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
		Ca:  s.CACert,
		Crt: certPEM,
	}, nil
}

func generateSerialNumber() (*big.Int, error) {
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)

	return rand.Int(rand.Reader, serialNumberLimit) //nolint:wrapcheck
}
