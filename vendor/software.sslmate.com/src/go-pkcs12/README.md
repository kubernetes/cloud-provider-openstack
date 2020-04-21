# package pkcs12

[![GoDoc](https://godoc.org/software.sslmate.com/src/go-pkcs12?status.svg)](https://godoc.org/software.sslmate.com/src/go-pkcs12)

    import "software.sslmate.com/src/go-pkcs12" 

Package pkcs12 implements some of PKCS#12 (also known as P12 or PFX).
It is intended for decoding P12/PFX files for use with the `crypto/tls`
package, and for encoding P12/PFX files for use by legacy applications which
do not support newer formats.  Since PKCS#12 uses weak encryption
primitives, it SHOULD NOT be used for new applications.

This package is forked from `golang.org/x/crypto/pkcs12`, which is frozen.
The implementation is distilled from https://tools.ietf.org/html/rfc7292
and referenced documents.

This repository holds supplementary Go cryptography libraries.

## Import Path

Note that although the source code and issue tracker for this package are hosted
on GitHub, the import path is:

    software.sslmate.com/src/go-pkcs12 

Please be sure to use this path when you `go get` and `import` this package.

## Download/Install

The easiest way to install is to run `go get -u software.sslmate.com/src/go-pkcs12`. You
can also manually git clone the repository to `$GOPATH/src/software.sslmate.com/src/go-pkcs12`.

## Report Issues / Send Patches

Open an issue or PR at https://github.com/SSLMate/go-pkcs12
