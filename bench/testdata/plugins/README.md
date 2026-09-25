# Plugin corpus (test fixtures)

The benchmarks and differential tests run the task plugins of
[new-api](https://github.com/QuantumNous/new-api)
(`plugins/tasks/<key>/plugin.js`). The plugins are licensed under AGPL-3.0
and are not part of this repository. `fetch.sh` downloads them at a pinned
commit into `<key>/plugin.js` here (git-ignored) and verifies each file:

```sh
bench/testdata/plugins/fetch.sh
```

Tests that need the plugins skip until they are fetched. The files are used
unmodified; the cgo adapters apply an in-memory `export` -> `globalThis`
rewrite so the scripts can run outside an ESM loader.
