# Vendored buildtools Parser

This directory contains a **subset** of [bazelbuild/buildtools](https://github.com/bazelbuild/buildtools).

## Contents

Only the following packages are included:

- `build/` - Starlark/BUILD file parser
- `labels/` - Bazel label parsing utilities
- `tables/` - Parser configuration tables

## License

This code is licensed under the Apache License 2.0, the same license as the original
buildtools repository. See the [LICENSE](LICENSE) file for details.

## Why Vendor?

This library vendors the parser to achieve **zero external runtime dependencies**.
Only the parser packages are needed; the full buildtools repository includes many
other tools (buildifier, buildozer, etc.) that are not required.

## Updating

To update the vendored code, run:

```bash
go run ./tools/vendor-parser -commit <commit-hash>
# or
go run ./tools/vendor-parser -tag <tag>
```

Then update all imports in the project if the destination path has changed.

## Version

See the [VERSION](VERSION) file for information about the vendored version.
