# Braidplane CLI

The Apache-2.0 command-line client for Braidplane. It manages API resources and
validates pipeline specifications locally. Braidplane is pre-v1; server features
and API compatibility depend on the server version you target.

## Build and test

Use the Go toolchain pinned in `go.mod`:

```sh
go test ./...
go build -trimpath -o braidplane ./cmd/braidplane
./braidplane --help
```

The module builds independently with `GOWORK=off`. It needs no private repository,
backend checkout, running server, or credentials for its unit tests. Live API
commands require a Braidplane account and API endpoint.

```sh
braidplane login --help
braidplane pipeline validate --local --file examples/multi-source-roots.example.yaml
```

Use the command help for authentication options. Saved authentication uses the
operating system credential store; do not put credentials into pipeline files.

## Source and generation

The Braidplane private monorepo is the canonical development source. This public
repository receives reviewed, one-way snapshots of its `cli/` directory, with
no private Git history. `canonical-source.json` records the canonical source
commit, subtree hash, and release version. Private `cli/vX.Y.Z` tags map to public
`vX.Y.Z` tags; prerelease suffixes are preserved.

The scoped schema in `api/schema.json` contains only the reviewed operations in
`api/operations.txt` and the wire types they reference. Regenerate the client and
shared enums without the private schema:

```sh
go tool oapi-codegen -config api/oapi-client.yml api/schema.json
go generate ./pkg/...
```

The shared packages in `pkg/` implement the same offline pipeline behavior used
by the backend. They include pipeline contracts, diffing, graph and SQL
validation, connector capability metadata, runtime-variable handling, and their
supporting types. They do not include backend services, credentials, persistence,
or the full private API schema. These pre-v1 Go packages are not a stable SDK.

## Releases

Release archives include the binary, license, dependency notices, this README and canonical source
receipt. Compare an archive with `checksums.txt` and verify its GitHub provenance:

```sh
gh attestation verify <archive> --repo Braidplane/cli
```

## Contributions

Public pull requests are not accepted for direct merge. Please open an issue for
bug reports and suggestions, excluding credentials and private operational data.
Maintainers implement accepted changes in the canonical monorepo and publish a
new reviewed snapshot. Changes made only in this repository would be overwritten
by the next snapshot.

## License

This module is licensed under Apache-2.0; see [LICENSE](LICENSE). Dependencies
retain their own licenses. The public module license does not license the private
Braidplane backend or grant trademark rights.
