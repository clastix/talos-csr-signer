// Copyright 2025 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

// Package errors contains the errors returned by the Kamaji Talos addon.
package errors

import (
	"errors"
)

var (
	// ErrDecodedCACertificate is the error when Certificate Authority decoding has failed.
	ErrDecodedCACertificate = errors.New("failed to decode CA certificate")
	// ErrMissingPort is the error when a zero value for port is defined.
	ErrMissingPort = errors.New("missing gRPC server port")
	// ErrPortOutOfRange is the error when a port is out of range.
	ErrPortOutOfRange = errors.New("gRPC server port is out of range")
	// ErrMissingToken is the error when a Talos token is not specified, required for the CSR process.
	ErrMissingToken = errors.New("missing Talos token")
	// ErrMissingPath is the error when a certificate component path is not declared.
	ErrMissingPath = errors.New("path is required")
	// ErrReadFile is the error when reading the certificate components from a path.
	ErrReadFile = errors.New("failed to read file")
	// ErrPemDecoding is the error when decoding the certificate PEM.
	ErrPemDecoding = errors.New("failed to decode PEM")
	// ErrParseCertificate is the error when parsing the certificate private key.
	ErrParseCertificate = errors.New("failed to parse private key")
	// ErrUnsupportedBlockType is the error when trying to parse a certificate with an unhandled block.
	ErrUnsupportedBlockType = errors.New("unsupported block type")
	// ErrLoadingCertificate is the error when loading the certificate from certificate and key from the FS.
	ErrLoadingCertificate = errors.New("failed to load certificate")
	// ErrServerListen is the error when the server can't start listening on the given port.
	ErrServerListen = errors.New("failed to listen on given port")
	// ErrGRPCServerServe is the error when the gRPC server is not hable to serve requests.
	ErrGRPCServerServe = errors.New("failed to serve gRPC")
)
