# Compute Node Layers — Typed Layers, Environments & Run Provenance

## Problem

Compute nodes already support **persistent layers** — read-only EFS directories that
survive across executions and are mounted into runs (built by
[`processor-build-python-layer`](https://github.com/Pennsieve/processor-build-python-layer)
via the `persistent-layer` data-target; see workflow `4c23010f`). Today a layer is an
opaque directory, but layers are used for two fundamentally different things:

1. **Reference data** — e.g. make the human genome (GRCh38) available to a workflow.
2. **Software environments** — a Python (`pip --target`) or R package set added to the
   interpreter's path so processors/notebooks can `import`/`library()` without baking
   everything into the base image.

These are identical at the *storage* layer but diverge in how they are built, wired into
a run, version-checked, and surfaced for provenance. Treating them as one untyped blob
means: no automatic `PYTHONPATH`/`R_LIBS` wiring (every consumer hardcodes the path), no
compatibility checks, and no way to record "what environment produced this result" — the
provenance question that matters most for a scientific platform.

## Goal

- Add an explicit **layer type** (`data` | `python-env` | `r-env`, extensible) that drives
  build, consume-side wiring, manifest schema, compatibility, and UX.
- Make a layer's contents **usable without consumer-side hardcoding** — the platform wires
  `PYTHONPATH` / `R_LIBS` / a data path based on the type.
- Capture, per run (workflow **and** notebook), the **fully resolved environment** that
  executed — base image + env layers + live package freeze — as an immutable provenance
  record, and surface it (and reproduce from it) in the app.
- Cleanly separate **software-environment provenance** from **reference-data provenance**.

## Design Decisions

- **Typed layers**: a `layerType` discriminator on the layer record and the
  `persistent-layer` data-target. `data` is the default (back-compat for existing untyped
  layers); `python-env` / `r-env` are explicit. Extensible (`conda-env`, `julia-env`…).
- **Storage stays generic**: all types are a read-only EFS dir at
  `/mnt/efs/{computeNodeId}/layers/{layerName}`; the type changes the *internal layout* and
  *how it's consumed*, not where it lives.
- **Consume-side wiring must APPEND, not clobber**: the converter injects a generic
  `LAYERS_DIR` today, and the **current contract is consumer self-wiring** — the processor
  reads `LAYERS_DIR` and does `sys.path.insert(...)` itself (e.g. `pennsieve-quick-plot`).
  Centralizing this (auto-set `PYTHONPATH`/`R_LIBS_USER`/`LAYER_<NAME>_DIR`) is desirable but
  **cannot be done via an ECS env override**: overrides *replace* the image's value (no
  `${PYTHONPATH}` shell-expansion), so they'd drop an image's own `PYTHONPATH` — e.g.
  `quick-plot`'s `ENV PYTHONPATH=/app`, breaking its imports. (A clobbering override was
  prototyped and rejected — see provisioner #34.) Safe auto-wiring needs a **runtime
  entrypoint wrapper** that appends, plus the layer's **python version** (from the manifest)
  to know the `lib/python{ver}/site-packages` subpath — so it's future work, not a converter
  one-liner. Until then, consumer self-wiring from `LAYERS_DIR` stands.
- **The image digest is the universal environment record**: a non-interactive processor
  is a Docker container, so its environment is fully defined by the **image digest +
  mounted env-layer versions** — language-agnostic (Python, R, Julia, C++, bash), already
  pinned, exact-reproducible by re-pull. A dependency **freeze is captured only for
  interactive notebooks**, where the kernel drifts at runtime (`pip install` mid-session)
  and the image alone under-describes what ran. General workflows need no language-specific
  capture.
- **Every layer carries a `manifest.json`, copied into the run** (not just referenced):
  for env layers it's a **recipe** (lockfile + interpreter version); for data layers it's
  an **identity + content fingerprint** (version, source, tree checksum). Copying it into
  the run record makes provenance self-contained and survives layer deletion.
- **Two provenance dimensions**: *software environment* (image digest + env layers + freeze)
  vs *inputs / reference data* (the primary dataset + data layers). The layer type decides
  which dimension a layer feeds — a genome is an input, not part of the software env.
- **At most one env layer per processor** (one `python-env` and one `r-env`); **many `data`
  layers** allowed. A `pip --target` tree is a *co-resolved* set; stacking two on
  `PYTHONPATH` causes silent version shadowing / ABI breaks (positional precedence is not a
  resolver). To combine env layers, rebuild one layer with the merged requirements so pip
  co-resolves them — never compose at runtime. Data layers are independent file trees with
  no resolution, so they compose freely. Enforced in `LayerNameSelector` (single-select for
  env layers) + validated at run submit.
- **Immutability**: env-layer provenance is only meaningful if a layer reference is stable.
  Resolve the current overwrite-vs-snapshot ambiguity in favor of versioned snapshots for
  env layers (see Open Questions).

---

## Layer Type Model

| Type | Internal layout | Built by | Consumed by | Manifest / lockfile | Interpreter-coupled |
|---|---|---|---|---|---|
| **`data`** (default) | opaque file tree | any producer (download/stage, e.g. a genome) | mount + `LAYER_<NAME>_DIR` env var | description, size, source, domain version (e.g. `GRCh38`), optional checksum | No |
| **`python-env`** | `lib/python{ver}/site-packages` (`pip install --target`) | `processor-build-python-layer` | prepend to `PYTHONPATH` (+ `bin/` → `PATH`) | `pip freeze`, `python_version`, `platform`/arch | Yes (wheel ABI) |
| **`r-env`** | R library dir (`library/`) | `processor-build-r-layer` (analogous, `renv`/`pak`) | prepend to `R_LIBS_USER` / `.libPaths()` | `renv.lock`, `r_version`, `platform` | Yes (package ABI) |

### What the type drives

1. **Build** — a type-specific builder processor produces the right internal layout. Python
   exists today; R is a direct analog; `data` is whatever stages the files.
2. **Consume-side wiring** *(the crux)* — the converter branches on type:
   - `data` → expose `LAYER_<NAME>_DIR=$LAYERS_DIR/{name}` (workflows reference reference-data by name).
   - `python-env` → `PYTHONPATH = $LAYERS_DIR/{name}/lib/python{ver}/site-packages : …` (+ `bin/`→`PATH`), and `PYTHONPYCACHEPREFIX=/tmp/pycache` so the read-only, `--no-compile` tree caches bytecode per-task instead of recompiling every import.
   - `r-env` → `R_LIBS_USER = $LAYERS_DIR/{name}/library : …`.
   - Multiple env layers of the same language **stack** in list order.
3. **Manifest schema** — discriminated by type (below).
4. **Compatibility** — only env layers: the layer's `python_version`/`r_version` + platform
   must match the consuming processor's interpreter, or the run fails at import. Validate at
   mount/config and surface in the UI. Data layers have no ABI coupling.
5. **UX** — a type badge; conditional rendering (env → language/version/packages; data →
   size/source/domain version); compatibility hints for env layers only.
6. **Provenance dimension** — env layers feed *software environment*; data layers feed
   *reference data*.

---

## Data model changes

### Layer record (`compute-node-layers`, workflow-service)
Add:
- `layerType: "data" | "python-env" | "r-env"` (default `data`)
- `version` / `snapshotId` (immutable snapshot identifier; see Open Questions)
- `manifestKey` — pointer to the layer's `manifest.json` on EFS/S3

(account-service mirrors the schema for the healthchecker reconcile — see
`internal/models/health.go` `LayerRecord` — and gains `layerType` there too.)

### `persistent-layer` data-target params
Today (`4c23010f`): `defaultParams.layerName`. Add:
- `layerType` — declares the type at commit time; validated against the builder that produced it.

### `manifest.json` (emitted by the builder, stored with the layer)
Common: `{ layerType, layerName, createdAt, builder: {sourceUrl, tag}, sizeBytes, fileCount }`

Type-specific:
- `python-env`: `{ pythonVersion, platform, packages: [{name, version}], pipFreeze: "<raw>" }`
- `r-env`: `{ rVersion, platform, renvLock: {...} }`
- `data`: `{ version, source, treeChecksum, files?: [{path, sizeBytes, sha256}] }` — an
  *identity + content fingerprint*, not a recipe. **Bound the inline `files` list**: always
  include `treeChecksum` + counts; include per-file entries only when `fileCount` is small
  (a genome has few large files; a million-file reference DB does not) — otherwise store the
  full file manifest as a separate artifact referenced by hash.

The manifest is the keystone: it lets the converter wire env layers correctly (knows the
`python{ver}` subpath), the UI render the catalog, consumers reproduce, and — copied into
each run — provenance survive layer deletion. `commit-layer` is the natural place to
generate it (it already computes `sizeBytes`/`fileCount`; add `treeChecksum`).

---

## Run-level environment provenance

The software environment is **fully defined by the container image digest + mounted
env-layer versions** — language-agnostic and already pinned. So capture is cheap and
*conditional on node type*; a dependency freeze is only needed where the environment is
mutable beyond the image.

| Node | Captured | Freeze? |
|---|---|---|
| Non-interactive processor (any language) | image digest + env-layer versions (+ data-layer identities) | **No** — the image *is* the environment; don't assume a Python/R interpreter exists |
| Interactive notebook | image digest + env-layer versions + **live `pip freeze`/`renv`** | **Yes** — the kernel drifts at runtime |

A non-interactive processor that runtime-installs deps is an **anti-pattern** (it escapes
the image); flag rather than chase it with a freeze — processors should bake deps into the
image.

**Capture mechanics:**
1. **At execution time** — record the image digest (`$ECS_CONTAINER_METADATA_URI_V4` →
   `Image` + digest), the mounted layer refs, **a copy of each mounted layer's
   `manifest.json`** (recipe for env layers, identity+fingerprint for data layers), the
   processor source commit (`sourceUrl` + `tag`), and — interactive only — the live freeze.
   Write `environment.json` to `/mnt/efs/{cni}/{eri}/logs/{nodeId}/`.
2. **At workflow end** — the finalizer collects per-node `environment.json` into a run-level
   provenance bundle on the run record.
3. **Notebooks** — freeze at **session end** (session-proxy/teardown) and embed it into the
   saved `.ipynb` metadata so the notebook is self-describing outside Pennsieve.

### Endpoint
`GET /compute/workflows/runs/{runId}/environment[?nodeId=]` — mirrors the existing per-node
logs endpoint (`/runs/{runId}/logs?nodeId=`) the app already uses. Backed by the run
provenance bundle.

### Provenance split
The run record exposes two dimensions; **the layer type routes each layer to one**:
- **Software environment** — image digest + `python-env`/`r-env` layers (+ interactive freeze). *"What code ran."*
- **Inputs / reference data** — the primary dataset (data-source, already tracked) + `data` layers (e.g. genome build), recorded by **identity** (version + source + tree checksum), not a recipe. *"What it ran against."*

### Provenance durability & layer lifecycle
Because each mounted layer's manifest is **copied into the run** (not just referenced),
deleting a layer never destroys a run's provenance — only its instant re-mount:

| | Manifest payload | Reproduce | If layer deleted |
|---|---|---|---|
| env layer | recipe (lockfile + version) | rebuild from lockfile | re-derive (best-effort; may drift if upstream packages vanish) |
| data layer | identity (version + source + tree checksum) | re-reference the bytes | re-fetch from `source`, verify by checksum |

So layer DELETE should either warn that referenced runs lose exact re-mount (reproduce →
rebuild/re-fetch), or **archive the layer to S3** so exact bytes stay recoverable. Hash-pin
hard (`--require-hashes` / `uv.lock` / `renv.lock`) + image digest gets most of the way to
exact without keeping files. *(Note current gap: `commit-layer` overwrites and gateway
`DELETE /layers/{name}` removes metadata with no run-reference guard and no copy-into-run.)*

### Reproduce
Image digest + the copied manifests = rebuildable. A "re-run with this environment" action
pins a new run to the captured image digest + layer versions (lockfile rebuild / data re-fetch as fallback).

---

## Self-service env layers (`requirements.txt` → build)

Today layer builds are **admin-driven**: each layer is a hand-authored workflow whose package
list is baked into terraform (`4c23010f`'s `defaultParams.REQUIREMENTS`) and triggered via
`POST /runs`. The builder (`processor-build-python-layer`) is already *generic* —
`REQUIREMENTS` is a parameter ("one image, many layers") — so flipping this to **self-service**
is mostly a user-facing trigger, not new infrastructure.

### Flow
```
User (app, on their compute node): layer name + requirements.txt + python version
  → account-service/workflow-service launches the build workflow on the node:
        trigger → python-layer-builder (REQUIREMENTS=<user list>, PYTHON_VERSION=<v>)
                → persistent-layer data-target (layerName=<user name>)
  → (Fargate, ~5–10 min)  layer committed → ComputeNodeLayers shows READY
  → user selects it for a workflow/notebook (LayerNameSelector)
```

### Reused vs. new
- **Reused**: the generic builder, the build-workflow shape, the layer catalog +
  create-layer form (`ComputeNodeLayers.vue` already takes `{name, description}`), the
  status badge (a `BUILDING` state fits the existing READY/pending badges).
- **New**: a build-trigger API that parameterizes the workflow from `requirements.txt` +
  python version and runs it on the user's node; build status tracking; manifest capture.

### Per-deployment-mode availability
Env-layer builds need PyPI (internet), so they're a basic/secure feature; compliant defines
its environment in the image instead. (Compliant has no internet — and no interactive nodes —
so there's nothing to mirror; the earlier "private PyPI mirror" question drops.)

| | basic / secure | compliant |
|---|---|---|
| **env layer** (pip build) | self-service | bake into the image |
| **data layer** (e.g. genome) | yes | yes — staged from S3 via the gateway endpoint (mount needs no internet) |
| **interactive notebook** | yes | no (already the case) |

### Decisions
- **Keep curated *and* self-service** — admin-curated shared stacks (the `quick-plot-stack`
  model) alongside user-built layers; don't drop curation.
- **Python-version compatibility** — the form must pick a version matching an available base
  image, or you build a `3.12` layer a `3.11` processor can't use (the manifest/compat concern).
- **Capture the resolved lock** — persist `pip freeze`, not just the input `requirements.txt`;
  show the user what actually resolved.
- **Versioning** — overwrite vs snapshot on rebuild, guarded by the active-run check.
- **Quota/GC** — builds cost Fargate, layers cost EFS; per-node quota + cleanup.

---

## UX (pennsieve-app, `Analysis/`)

Surfaces already exist; provenance attaches by extending them.

- **`RunMonitor/RunDetail.vue`** *(MVP)* — clone the per-node **logs dialog**
  (`openLogs` → logs endpoint) into an **Environment** dialog (`openEnvironment` → the new
  environment endpoint). Per-node + a run-level summary. Shows base image, python/R version,
  env layers (link to catalog), searchable package list, download lock, and "re-run with this
  environment". Reference-data layers shown in a separate section.
- **`ComputeNodes/ComputeNodeLayers.vue`** — the layers table exists (name/status/size/files);
  add a **type badge** + manifest detail (env → language/version/packages via row-expand; data
  → source/domain version), and "used by N workflows".
- **`RunMonitor/LayerNameSelector.vue`** — filter/group by type; env preview + compatibility
  hint (layer interpreter vs node base) when selecting.
- **`JupyterSession/NotebookSession.vue`** — "snapshot environment" / "save as layer", a drift
  indicator, and `.ipynb` freeze embedding.

---

## Cross-repo touchpoints

| Concern | Repo |
|---|---|
| `layerType` + manifest on the layer record; active-run/layer reconcile | account-service (`internal/models/health.go`, healthchecker) + workflow-service (owner of `compute-node-layers`) |
| `persistent-layer` data-target `layerType` param; `commit-layer`/`mount-layers`; converter env wiring (`PYTHONPATH`/`R_LIBS`/`LAYER_<NAME>_DIR`); run-env capture in `preDestroy`/finalizer; `manifest.json` handling | compute-node-aws-provisioner-v2 (`cmd/data-transfer`, `internal/aslconverter`, `cmd/workflow-finalizer`, gateway) |
| `manifest.json` emission | processor-build-python-layer; new processor-build-r-layer |
| Run-environment endpoint | gateway (`compute-gateway-v2`) / workflow-service |
| Layer catalog, env dialog, selector, notebook snapshot | pennsieve-app (`src/components/Analysis/…`) |

---

## Phasing

1. **Type model** — add `layerType` to the layer record + `persistent-layer` data-target;
   default `data`. No behavior change yet.
2. **Python env, end-to-end** — `manifest.json` from the build processor (incl. `pythonVersion`);
   safe auto-wiring via a **runtime entrypoint wrapper** that *appends* the layer's
   `lib/python{ver}/site-packages` to `PYTHONPATH` (+ `PYTHONPYCACHEPREFIX=/tmp/pycache`) —
   not a clobbering ECS env override (see Consume-side wiring); compatibility check. Until
   this lands, consumers self-wire from `LAYERS_DIR`.
3. **Run-environment capture + endpoint** — entrypoint freeze + image digest + layer refs →
   finalizer bundle → `GET /runs/{id}/environment`.
4. **App MVP** — RunDetail Environment dialog (read provenance) + ComputeNodeLayers type badge.
5. **Self-service env layers** — `requirements.txt` → build-trigger API + ComputeNodeLayers
   create-from-requirements form + build status (basic/secure).
6. **Data layers** — `LAYER_<NAME>_DIR` wiring (trivial — just mount) + reference-data
   provenance section.
7. **R env** — `processor-build-r-layer` + `R_LIBS_USER` wiring + `renv.lock` manifest.
8. **Reproduce + notebook snapshot/`.ipynb` embedding**; optional Discover provenance.

---

## Open Questions

1. **Immutability/versioning** — `commit-layer` currently overwrites; the README claims
   layers are immutable. For env layers (where run provenance references them) pick versioned
   snapshots (`{name}@{hash}`) and have the converter pin consumers; data layers may tolerate
   overwrite. Decide and make consistent.
2. **Compatibility enforcement** — hard-fail vs warn-and-proceed on python/R version or arch
   mismatch at mount/config.
3. **EFS import latency** — env layers imported off NFS are slow; position as incremental deps
   on a curated base image, and/or stage to local ephemeral storage at task start. Decide
   whether to invest in local staging now.
4. **Compliant mode** — *resolved*: env-layer builds need PyPI/CRAN (internet), so they're
   basic/secure only; compliant defines its environment in the image (no mirror needed). Data
   layers still work in compliant (S3-staged via the gateway endpoint).
5. **Provenance home** — run record only, or promote the environment + reference-data record
   into the published dataset's provenance in Discover (highest reproducibility value).
6. **Reproduce scope** — capture-only, or a first-class "rebuild environment from a run's lock"
   action.
