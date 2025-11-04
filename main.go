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

const (
	cliPortName           = "port"
	cliCACertificatePath  = "ca-cert-path"
	cliCAPrivateKeyPath   = "ca-key-path"
	cliTLSCertificatePath = "tls-cert-path"
	cliTLSPrivateKeyPath  = "tls-key-path"
	cliTalosToken         = "talos-token"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "talos-csr-signer",
		Short: "gRPC server for signing Talos CSR",
		PreRunE: func(*cobra.Command, []string) error {
			switch {
			case viper.GetInt(cliPortName) <= 0:
				return pkgerrors.ErrMissingPort
			case viper.GetInt(cliPortName) > 65535:
				return pkgerrors.ErrPortOutOfRange
			case viper.GetString(cliTalosToken) == "":
				return pkgerrors.ErrMissingToken
			case viper.GetString(cliCACertificatePath) == "":
				return errors.Wrap(pkgerrors.ErrMissingPath, "CA certificate path is missing")
			case viper.GetString(cliCAPrivateKeyPath) == "":
				return errors.Wrap(pkgerrors.ErrMissingPath, "CA private key path is missing")
			case viper.GetString(cliTLSCertificatePath) == "":
				return errors.Wrap(pkgerrors.ErrMissingPath, "server certificate path is missing")
			case viper.GetString(cliTLSPrivateKeyPath) == "":
				return errors.Wrap(pkgerrors.ErrMissingPath, "server private key path is missing")
			}

			return nil
		},
		RunE: func(*cobra.Command, []string) error {
			// Load CA certificate
			caCertPEM, caCertErr := os.ReadFile(viper.GetString(cliCACertificatePath))
			if caCertErr != nil {
				return errors.Wrap(pkgerrors.ErrReadFile, "failed to read CA certificate: "+caCertErr.Error())
			}
			// Load CA private key
			caKeyPEM, caKeyErr := os.ReadFile(viper.GetString(cliCAPrivateKeyPath))
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

			cert, crtErr := tls.LoadX509KeyPair(viper.GetString(cliTLSCertificatePath), viper.GetString(cliTLSPrivateKeyPath))
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
				ValidToken:   viper.GetString(cliTalosToken),
			}

			port := viper.GetInt(cliPortName)
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
	rootCmd.Flags().Int(cliPortName, 50001, "Port to listen on")
	rootCmd.Flags().String(cliCACertificatePath, "/etc/talos-ca/tls.crt", "Path to CA certificate")
	rootCmd.Flags().String(cliCAPrivateKeyPath, "/etc/talos-ca/tls.key", "Path to CA private key")
	rootCmd.Flags().String(cliTLSCertificatePath, "/etc/talos-server-crt/tls.crt", "Path to the Server TLS certificate")
	rootCmd.Flags().String(cliTLSPrivateKeyPath, "/etc/talos-server-crt/tls.key", "Path to Server TLS private key")
	rootCmd.Flags().String(cliTalosToken, "", "Talos token")
	// Bind flags to viper keys
	_ = viper.BindPFlag(cliPortName, rootCmd.Flags().Lookup(cliPortName))
	_ = viper.BindPFlag(cliCACertificatePath, rootCmd.Flags().Lookup(cliCACertificatePath))
	_ = viper.BindPFlag(cliCAPrivateKeyPath, rootCmd.Flags().Lookup(cliCAPrivateKeyPath))
	_ = viper.BindPFlag(cliTLSCertificatePath, rootCmd.Flags().Lookup(cliTLSCertificatePath))
	_ = viper.BindPFlag(cliTLSPrivateKeyPath, rootCmd.Flags().Lookup(cliTLSPrivateKeyPath))
	_ = viper.BindPFlag(cliTalosToken, rootCmd.Flags().Lookup(cliTalosToken))
	// Allow reading from env variables automatically. Env keys are uppercased and `.` replaced with `_`.
	viper.SetEnvPrefix("")
	viper.AutomaticEnv()
	// Explicit env key mapping (to allow different names if desired)
	_ = viper.BindEnv(cliPortName, "PORT")
	_ = viper.BindEnv(cliCACertificatePath, "CA_CERT_PATH")
	_ = viper.BindEnv(cliCAPrivateKeyPath, "CA_KEY_PATH")
	_ = viper.BindEnv(cliTLSCertificatePath, "TLS_CERT_PATH")
	_ = viper.BindEnv(cliTLSPrivateKeyPath, "TLS_KEY_PATH")
	_ = viper.BindEnv(cliTalosToken, "TALOS_TOKEN")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1) //nolint:gocritic
	}
}
