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
Configure the API endpoint directly: authenticated requests do not follow HTTP
redirects, so credentials and request bodies stay with the configured endpoint.

```sh
braidplane login --help
braidplane pipeline validate --local --file examples/multi-source-roots.example.yaml
```

Use the command help for authentication options. Saved authentication uses the
operating system credential store; do not put credentials into pipeline files.

## ODCS contract catalog

The project-owned ODCS catalog records immutable canonical JSON revisions.
Use `contract import` below for Git-authored YAML/JSON artifacts. To record a JSON
document directly, create a catalog identity and then record its revision:

```sh
braidplane contract create --project data-pipelines --name Orders --slug orders --odcs-id acme.orders
braidplane contract revision create orders --project data-pipelines --slug v1 --file orders.odcs.json
```

List or fetch contracts and revisions as JSON. Revision responses include their
exact slug and digest. To inspect known in-project pipeline consumers and safe
compatibility diagnostics before changing a reference, use:

```sh
braidplane contract revision impact orders --project data-pipelines --from v1 --to v2
```

Inspect a complete admitted document and static declaration coverage explicitly:

```sh
braidplane contract revision inspect orders v1 --project data-pipelines --object order
```

This requires project read access and can include descriptive contact/team data.
The JSON is inert; links/instructions are not executed and variables remain literal.
Coverage is scoped to the exact revision digest and object. `enforced` means compiled
constraints, never a passed execution. Omitting `--object` selects no executable
object; coverage and missing-selection diagnostics remain inspectable. Routine
revision get/create output remains metadata-only.

The impact response reports only known project consumers; external consumers
remain explicitly unknown.

### Import Git-authored ODCS agreements

`braidplane contract import --project PROJECT` previews supported YAML/JSON files
under `contracts/` in the configured project Git backend. Review the manifest,
then use `--apply --expected-commit FULL_PREVIEW_COMMIT` to record that exact
snapshot. Repeated imports reuse immutable revisions; imports never upgrade
pipeline references. Versions and dates must be quoted strings in YAML; aliases,
merge keys and multiple documents are rejected. Original bytes and their digest
remain separate from canonical JSON revisions under project read permissions.
Removing a file retains history. Archive or reactivate its identity explicitly
with `contract update --status archived` or `--status active`, also supplying
`--name`.

## Managed-source Proof settings

Read a managed GitHub or GitLab source's settings, status, and available target
contexts using your configured organization and authentication:

```sh
braidplane proof configuration get managed-source
```

To replace its settings, save an explicit request as `proof-settings.json`:

```json
{
  "enabled": true,
  "publish_checks": false,
  "expected_repository_id": "repository-id-from-get",
  "targets": [
    {
      "project_slug": "ops",
      "environment_slug": "prod",
      "target_ref": "main",
      "pipeline_path": "pipelines/orders.yaml"
    }
  ]
}
```

Replace the example repository identity and target with values from `get`.
Choose a pipeline path inside the available target's `pipeline_directory`.

```sh
braidplane proof configuration set managed-source --file proof-settings.json
```

All four top-level fields are required and cannot be null. The command replaces
the complete target list; include every target you want to keep. The API checks
the expected repository identity when enabling assessments and rejects stale
repository selections or invalid targets. Set `enabled` to false to disable
assessments; the API preserves stored targets and receipt history. Publication
is independent: `publish_checks` opts into informational GitHub checks or GitLab
notes when the server permits publication. These settings do not authorize live
connector observations.

Reading requires organization read permissions for data sources and pipelines;
writing requires update permissions for both. API keys and PATs keep their
existing permissions. A read-only key receives an error on writes. OAuth
reconnection stays in the provider's account consent flow.

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
