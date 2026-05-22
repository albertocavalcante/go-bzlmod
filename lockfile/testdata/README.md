# Real-corpus MODULE.bazel.lock fixtures

These are unmodified `MODULE.bazel.lock` files from real Bazel
projects. They exist so the `lockfile` package's tests run against
the actual JSON shape Bazel emits, not just synthesized snippets
that happened to match an assumed model. Their presence caught the
`ModuleExtensionData` wrapper bug (the model expected a double-
nested `general` key that Bazel does not emit).

| File                     | Lockfile version | Source                                              |
| ------------------------ | ---------------- | --------------------------------------------------- |
| `v13_rules_go.lock`      | 13               | `github.com/bazel-contrib/rules_go` (snapshot)      |
| `v14_rules_scala.lock`   | 14               | `github.com/bazelbuild/rules_scala` (snapshot)      |
| `v18_bazel_gazelle.lock` | 18               | `github.com/bazel-contrib/bazel-gazelle` (snapshot) |
| `v26_bazel.lock`         | 26               | `github.com/bazelbuild/bazel` (snapshot)            |

The four versions span every meaningful schema increment between
Bazel 6.x and the latest Bazel 9 / rolling. If you're adding a new
schema version, add a fixture from a real project pinning that
version (don't synthesize — the bug above proved synthesized data
isn't sufficient).

To refresh, locate a workspace using the target Bazel version and
copy its `MODULE.bazel.lock` verbatim. Don't trim — the full file
includes registry hash maps, recorded inputs, and other fields whose
shape we want to keep tested.
