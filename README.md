# Braidplane CLI

The Apache-2.0 command-line client for Braidplane. It manages API resources and
validates pipeline specifications locally. Braidplane is pre-v1; server features
and API compatibility depend on the server version you target.

## Install

Download the archive for your operating system and architecture from
[Releases](https://github.com/Braidplane/cli/releases). Extract it and put the
`braidplane` executable on your `PATH`.

To build from source, clone this repository and use the Go toolchain pinned in
`go.mod`:

```sh
go build -trimpath -o braidplane ./cmd/braidplane
./braidplane --help
```

## Usage

Live API commands require a Braidplane account and API endpoint. Local pipeline
validation does not require a server or credentials.

```sh
braidplane login --help
braidplane pipeline validate --local --file examples/multi-source-roots.example.yaml
```

Use the command help for authentication options. Saved authentication uses the
operating system credential store; do not put credentials into pipeline files.

## Releases

Release archives include the binary, license, dependency notices, this README and canonical source
receipt. Compare an archive with `checksums.txt` and verify its GitHub provenance:

```sh
gh attestation verify <archive> --repo Braidplane/cli
```

## Contributions

Public pull requests are not accepted for direct merge. Please open an issue for
bug reports and suggestions, excluding credentials and private operational data.

## License

This module is licensed under Apache-2.0; see [LICENSE](LICENSE). Dependencies
retain their own licenses. This license covers the CLI, not other Braidplane
products, and does not grant trademark rights.
