// Copyright 2025 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

// Run the gRPC Server as a CLI binary.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pkgerrors "github.com/clastix/talos-csr-signer/pkg/errors"
	pb "github.com/clastix/talos-csr-signer/pkg/proto"
	"github.com/clastix/talos-csr-signer/pkg/server"
)

func main() {
	var port int

	var caCertPath, caKeyPath, tlsCertPath, tlsKeyPath, token string

	rootCmd := &cobra.Command{
		Use:   "talos-csr-signer",
		Short: "gRPC server for signing Talos CSR",
		PreRunE: func(*cobra.Command, []string) error {
			switch {
			case port <= 0:
				return pkgerrors.ErrMissingPort
			case port > 65535:
				return pkgerrors.ErrPortOutOfRange
			case token == "":
				return pkgerrors.ErrMissingToken
			case caCertPath == "":
				return errors.Wrap(pkgerrors.ErrMissingPath, "CA certificate path is missing")
			case caKeyPath == "":
				return errors.Wrap(pkgerrors.ErrMissingPath, "CA private key path is missing")
			case tlsCertPath == "":
				return errors.Wrap(pkgerrors.ErrMissingPath, "server certificate path is missing")
			case tlsKeyPath == "":
				return errors.Wrap(pkgerrors.ErrMissingPath, "server private key path is missing")
			}

			return nil
		},
		RunE: func(*cobra.Command, []string) error {
			// Load CA certificate
			caCertPEM, caCertErr := os.ReadFile(caCertPath) //nolint:gosec
			if caCertErr != nil {
				return errors.Wrap(pkgerrors.ErrReadFile, "failed to read CA certificate: "+caCertErr.Error())
			}
			// Load CA private key
			caKeyPEM, caKeyErr := os.ReadFile(caKeyPath) //nolint:gosec
			if caKeyErr != nil {
				return errors.Wrap(pkgerrors.ErrReadFile, "failed to read CA private key: "+caKeyErr.Error())
			}
			// Parse CA private key
			block, _ := pem.Decode(caKeyPEM)
			if block == nil {
				return pkgerrors.ErrPemDecoding
			}

			var caPrivateKey interface{}
			var privateKeyErr error

			switch block.Type {
			case "ED25519 PRIVATE KEY":
				caPrivateKey, privateKeyErr = x509.ParsePKCS8PrivateKey(block.Bytes)
			case "EC PRIVATE KEY":
				caPrivateKey, privateKeyErr = x509.ParseECPrivateKey(block.Bytes)
			case "RSA PRIVATE KEY":
				caPrivateKey, privateKeyErr = x509.ParsePKCS1PrivateKey(block.Bytes)
			case "PRIVATE KEY":
				caPrivateKey, privateKeyErr = x509.ParsePKCS8PrivateKey(block.Bytes)
			default:
				return errors.Wrap(pkgerrors.ErrUnsupportedBlockType, block.Type)
			}

			if privateKeyErr != nil {
				return errors.Wrap(pkgerrors.ErrParseCertificate, privateKeyErr.Error())
			}

			cert, crtErr := tls.LoadX509KeyPair(tlsCertPath, tlsKeyPath)
			if crtErr != nil {
				return errors.Wrap(pkgerrors.ErrLoadingCertificate, crtErr.Error())
			}

			// Create TLS credentials
			tlsConfig := &tls.Config{ //nolint:gosec
				Certificates: []tls.Certificate{cert},
				ClientAuth:   tls.NoClientCert, // Don't require client certificates
			}
			creds := credentials.NewTLS(tlsConfig)

			// Create gRPC Server with TLS
			srv := &server.Server{
				CACert:       caCertPEM,
				CAPrivateKey: caPrivateKey,
				ValidToken:   token,
			}

			lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
			if err != nil {
				return errors.Wrap(pkgerrors.ErrServerListen, fmt.Sprintf("%d: %s", port, err.Error()))
			}

			grpcServer := grpc.NewServer(grpc.Creds(creds))
			pb.RegisterSecurityServiceServer(grpcServer, srv)

			log.Printf("Talos CSR Signer listening on port %d with TLS enabled", port)

			if err = grpcServer.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
				return errors.Wrap(pkgerrors.ErrGRPCServerServe, err.Error())
			}

			return nil
		},
	}

	// Flags with their defaults
	rootCmd.Flags().IntVar(&port, "port", 50001, "Port to listen on")
	rootCmd.Flags().StringVar(&caCertPath, "ca-cert-path", "/etc/talos-ca/ca.crt", "Path to CA certificate")
	rootCmd.Flags().StringVar(&caKeyPath, "ca-key-path", "/etc/talos-ca/ca.key", "Path to CA private key")
	rootCmd.Flags().StringVar(&tlsCertPath, "tls-cert-path", "/etc/talos-server-crt/tls.crt", "Path to the Server TLS certificate")
	rootCmd.Flags().StringVar(&tlsKeyPath, "tls-key-path", "/etc/talos-server-crt/tls.key", "Path to Server TLS private key")
	rootCmd.Flags().StringVar(&token, "talos-token", "", "Talos token")
	// Bind flags to viper keys
	_ = viper.BindPFlag("port", rootCmd.Flags().Lookup("port"))
	_ = viper.BindPFlag("ca_cert_path", rootCmd.Flags().Lookup("ca-cert-path"))
	_ = viper.BindPFlag("ca_key_path", rootCmd.Flags().Lookup("ca-key-path"))
	_ = viper.BindPFlag("tls_cert_path", rootCmd.Flags().Lookup("tls-cert-path"))
	_ = viper.BindPFlag("tls_key_path", rootCmd.Flags().Lookup("tls-key-path"))
	_ = viper.BindPFlag("talos_token", rootCmd.Flags().Lookup("talos-token"))
	// Allow reading from env variables automatically. Env keys are uppercased and `.` replaced with `_`.
	viper.SetEnvPrefix("")
	viper.AutomaticEnv()
	// Explicit env key mapping (to allow different names if desired)
	_ = viper.BindEnv("port", "PORT")
	_ = viper.BindEnv("ca_cert_path", "CA_CERT_PATH")
	_ = viper.BindEnv("ca_key_path", "CA_KEY_PATH")
	_ = viper.BindEnv("tls_cert_path", "TLS_CERT_PATH")
	_ = viper.BindEnv("tls_key_path", "TLS_KEY_PATH")
	_ = viper.BindEnv("talos_token", "TALOS_TOKEN")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1) //nolint:gocritic
	}
}
