# Pinned OpenAPI validator

This small standalone Go module validates the plan's OpenAPI document with
`libopenapi-validator v0.16.1` and `libopenapi v0.41.3`. Its parser ignores
array-based circular-reference detection because the contract includes the
finite recursive `GroupTreeNode.children` shape. It keeps other reference
checks and the full OpenAPI document validation enabled.

Run from this directory:

```sh
go run . ../../openapi.yaml
```

The first run downloads the pinned Go modules into the local Go module cache.
