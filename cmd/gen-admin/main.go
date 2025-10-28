package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"time"
)

func main() {
	caCertB64 := "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUJQakNCOGFBREFnRUNBaEEwSWNzakxrWC9zV1ZCdW1wRW9xditNQVVHQXl0bGNEQVFNUTR3REFZRFZRUUsKRXdWMFlXeHZjekFlRncweU5URXdNamd3T0RRNE1UZGFGdzB6TlRFd01qWXdPRFE0TVRkYU1CQXhEakFNQmdOVgpCQW9UQlhSaGJHOXpNQ293QlFZREsyVndBeUVBL2huekd4d1BOaUdPNFhnNFkrUWV4ZC8wb29wSStCMkpaenpmCmtIOHY4dStqWVRCZk1BNEdBMVVkRHdFQi93UUVBd0lDaERBZEJnTlZIU1VFRmpBVUJnZ3JCZ0VGQlFjREFRWUkKS3dZQkJRVUhBd0l3RHdZRFZSMFRBUUgvQkFVd0F3RUIvekFkQmdOVkhRNEVGZ1FVa0pxK3Jhb1F1elA1RThOKwpaWFRTWDlpQzZXUXdCUVlESzJWd0EwRUFpV01CZnBBVlIxMTQyTXdVd08zZDBTQ2ZTR3ZIamw5TEtDWTBTM3hLCjlMMWRCWFI1TDdFY1NWUnZJUGxVbkN4dUx6Y2VmcjlUTGx2SkxNOVF5cWt1QkE9PQotLS0tLUVORCBDRVJUSUZJQ0FURS0tLS0tCg=="
	caKeyB64 := "LS0tLS1CRUdJTiBFRDI1NTE5IFBSSVZBVEUgS0VZLS0tLS0KTUM0Q0FRQXdCUVlESzJWd0JDSUVJSXFidDBCbGpEb0cyc3RNRTN1TXMzQVpRRXRXbUdOa3NrZTEwOFF0c29NcAotLS0tLUVORCBFRDI1NTE5IFBSSVZBVEUKS0VZLS0tLS0K"

	caCertPEM, err := base64.StdEncoding.DecodeString(caCertB64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to decode CA cert: %v\n", err)
		os.Exit(1)
	}

	caKeyPEM, err := base64.StdEncoding.DecodeString(caKeyB64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to decode CA key: %v\n", err)
		os.Exit(1)
	}

	caBlock, _ := pem.Decode(caCertPEM)
	if caBlock == nil {
		fmt.Fprintf(os.Stderr, "Failed to decode CA cert PEM\n")
		os.Exit(1)
	}

	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse CA cert: %v\n", err)
		os.Exit(1)
	}

	caKeyBlock, _ := pem.Decode(caKeyPEM)
	if caKeyBlock == nil {
		fmt.Fprintf(os.Stderr, "Failed to decode CA key PEM\n")
		os.Exit(1)
	}

	caKey, err := x509.ParsePKCS8PrivateKey(caKeyBlock.Bytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse CA key: %v\n", err)
		os.Exit(1)
	}

	adminKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to generate admin key: %v\n", err)
		os.Exit(1)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to generate serial: %v\n", err)
		os.Exit(1)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"os:admin"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &adminKey.PublicKey, caKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create certificate: %v\n", err)
		os.Exit(1)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	certB64 := base64.StdEncoding.EncodeToString(certPEM)

	keyBytes, err := x509.MarshalECPrivateKey(adminKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal key: %v\n", err)
		os.Exit(1)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	keyB64 := base64.StdEncoding.EncodeToString(keyPEM)

	fmt.Printf("%s\n%s\n", certB64, keyB64)
}
