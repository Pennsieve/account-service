# Compute Node Layers & Environment Provenance — Implementation Steps

Companion to [`compute-node-layers-plan.md`](./compute-node-layers-plan.md) (the *what/why*).
This is the *how/in-what-order* — a reviewable work breakdown. Each milestone lists the
repos/files touched, acceptance criteria, and the decisions it forces. Milestones are
ordered by dependency; several are independently shippable.

> Scope note: the *current* compute-node project (interactive notebooks + delete robustness)
> is done, and layers already work via the **consumer-self-wiring** contract (`read
> LAYERS_DIR → sys.path`). Everything below is the **environments & provenance initiative** —
> a deliberate follow-on, not a hotfix.

---

## Decisions (settled)

Agreed before build; these shape multiple milestones.

| # | Decision | **Settled** |
|---|---|---|
| D1 | **Layer versioning** | **Immutable snapshots (`{name}@{hash}`) for `python-env`/`r-env`** (a run references a stable env); **`data` layers overwrite in place**, guarded by an active-run check. **Snapshot id = short SHA-256 of the normalized resolved lock** (sorted `pip freeze`), computed at commit — content-addressed (rebuilding identical deps dedups; PyPI drift yields a new id). Hash the *resolved* lock, not the input `requirements.txt`. Manifest keeps `treeChecksum` for byte-integrity. |
| D2 | **Compatibility enforcement** | **Hard-fail at run submit** on python/R version (or arch) mismatch; **warn earlier in the selector**. |
| D3 | **Consume-side wiring** | **Keep consumer self-wiring from `LAYERS_DIR` as the contract; add an opt-in runtime entrypoint wrapper** that auto-appends `PYTHONPATH` from the manifest (never a clobbering env override). |
| D4 | **Provenance home** | **Run record for v1; Discover dataset provenance later.** |
| D5 | **EFS import perf** | **Accept NFS imports for v1** (base-image + thin-layer posture); measure cold-start, optimize (local staging) only if it hurts. |
| D6 | **Quota / GC** | **No cap for v1** — ship self-service without quotas (admin/power-user-facing at first); add per-node limits + cleanup later if EFS/cost becomes a problem. |

---

## M1 — Layer type model (foundation)

**Goal:** every layer carries a `layerType`; existing layers default to `data`. No behavior
change yet.

**Tasks**
- workflow-service (`compute-node-layers` store/model): add `layerType` (default `data`),
  `manifestKey`, `version`/`snapshotId`. Migration: backfill existing rows → `data`.
- provisioner `cmd/data-transfer`: `persistent-layer` data-target accepts a `layerType`
  param (alongside `layerName`); `commit-layer` persists it.
- account-service `internal/models/health.go` (`LayerRecord`) + healthchecker reconcile:
  mirror `layerType`.

**Acceptance:** a newly committed layer records its `layerType`; existing layers read back as
`data`; reconcile passes.

**Decisions:** D1 (version field shape).

---

## M2 — Layer manifest (`python-env`)

**Goal:** a built python-env layer self-describes (versions + python version + checksum).

**Tasks**
- processor-build-python-layer: emit `manifest.json` — `pythonVersion`, `platform`,
  `packages` (from `pip freeze`), raw freeze. (Already a README TODO.)
- provisioner `commit-layer`: store the builder's `manifest.json` with the layer; add
  `sizeBytes`/`fileCount` (already computed) + `treeChecksum`. Write `manifestKey` to the record.
- workflow-service: surface `manifestKey` + parsed summary on the layer record / list API.

**Acceptance:** rebuilding `quick-plot-stack` produces a `manifest.json` with resolved
versions + `pythonVersion: 3.12`, readable via the layers API.

**Depends on:** M1. **Decisions:** D1.

---

## M3 — Safe consume-side wiring (runtime wrapper)

**Goal:** a `python-env` layer is importable without consumer hardcoding, **without
clobbering** the image's own `PYTHONPATH`.

**Mechanism (settled):** an **in-image entrypoint wrapper**, not a converter override. A
Pennsieve processor base image (or a drop-in 2-line entrypoint snippet) that, before exec'ing
the real command: reads `LAYERS_DIR` (already injected — *no converter change*), **globs** each
mounted layer's `lib/python*/site-packages` and **appends** them to `PYTHONPATH`, sets
`PYTHONPYCACHEPREFIX=/tmp/pycache`, then `exec "$@"`. Runtime shell expansion = append-safe
(preserves the image's own `PYTHONPATH`); the glob makes it version-agnostic (no need to thread
the manifest version through the converter). **Opt-in** (D3): processors on the base/snippet get
auto-wiring; others keep the `LAYERS_DIR` self-wiring contract.

**Tasks**
- New Pennsieve processor **base image** (or entrypoint snippet) implementing the glob+append
  wrapper above.
- Compatibility check (D2): hard-fail at run submit when a layer's `pythonVersion` (manifest) ≠
  the processor's; warn in the selector.
- Migrate `processor-build-python-layer` / `pennsieve-quick-plot` onto the base (or add the
  snippet) to adopt — net-new processors start on it.

**Acceptance:** a processor on the base image with a `python-env` layer imports its packages with
no hardcoding, **and** an image that sets its own `PYTHONPATH` (e.g. `quick-plot`'s `/app`) still
works (append, not clobber — regression guard).

**Depends on:** M2. **Decisions:** D2, D3.
*(Reference: the clobbering ECS-override approach was prototyped and rejected — provisioner #34; this in-image append wrapper is the fix.)*

---

## M4 — Run-environment capture + endpoint

**Goal:** every run records the exact environment that executed.

**Tasks**
- provisioner (entrypoint capture, uniform): image digest (`$ECS_CONTAINER_METADATA_URI_V4`),
  mounted layer refs + **copied manifests**, processor source commit, and — interactive only —
  live `pip freeze`. Write `environment.json` to `/mnt/efs/{cni}/{eri}/logs/{nodeId}/`.
- provisioner `cmd/workflow-finalizer`: collect per-node `environment.json` → run-level
  provenance bundle on the run record (workflow-service).
- endpoint: `GET /compute/workflows/runs/{runId}/environment[?nodeId=]` (gateway /
  workflow-service) — mirrors the existing per-node logs endpoint.

**Acceptance:** a completed non-interactive run exposes image digest + layer versions; an
interactive session additionally exposes the live freeze. Survives layer deletion (manifests
are copied, not referenced).

**Depends on:** M2 (manifests to copy). **Decisions:** D4.

---

## M5 — App MVP (view provenance)

**Goal:** users can see a run's environment and browse layer details.

**Tasks**
- pennsieve-app `RunMonitor/RunDetail.vue`: clone the per-node **logs dialog**
  (`openLogs`) into an **Environment** dialog (`openEnvironment` → M4 endpoint); per-node +
  run-level; base image, versions, env layers, searchable package list, download lock.
- pennsieve-app `ComputeNodes/ComputeNodeLayers.vue` + `computeResourcesStore`: **type
  badge** + manifest detail (row-expand).

**Acceptance:** from a run, a user opens "Environment" and sees what ran; the layer catalog
shows type + contents.

**Depends on:** M4 (endpoint), M2 (manifest for the catalog). **Decisions:** —

---

## M6 — Self-service env layers (`requirements.txt` → build)

**Goal:** a user creates a python-env layer from a `requirements.txt` (basic/secure only).

**Tasks**
- account-service / workflow-service: build-trigger API — `{computeNode, layerName,
  requirements.txt, pythonVersion}` → launch the build workflow (`python-layer-builder` +
  `persistent-layer` target) on the node; track build status.
- pennsieve-app `ComputeNodeLayers.vue`: extend the create-layer form to
  `{name, requirements, pythonVersion}`; show a `BUILDING` state on the existing badge.
- No quota/GC for v1 (D6) — ships without per-node caps; revisit if EFS/cost grows.

**Acceptance:** user submits a `requirements.txt` → layer builds on Fargate → shows READY →
selectable; resolved lock captured (M2); rebuild creates a new snapshot (D1). Rejected on
compliant nodes with a clear message.

**Depends on:** M1, M2. **Decisions:** D1 (snapshot on rebuild).

---

## M7 — Data layers (reference data)

**Goal:** `data` layers are referenceable and show up as inputs, not environment.

**Tasks**
- provisioner converter: per-mounted-`data`-layer `LAYER_<NAME>_DIR` env var.
- provenance: route `data` layers to the **inputs/reference-data** dimension of the run
  record (identity from the manifest — version/source/treeChecksum).
- (compliant) S3-staging path to populate a data layer's EFS dir via the gateway endpoint.

**Acceptance:** a workflow references a genome via `LAYER_GENOME_DIR`; the run's provenance
shows it under reference data by identity.

**Depends on:** M1, M4. **Decisions:** —

---

## M8 — Selection rules + UX

**Goal:** enforce "one env layer per processor; many data layers."

**Tasks**
- pennsieve-app `RunMonitor/LayerNameSelector.vue`: single-select for env layers, multi-select
  for data; compatibility hint (D2).
- workflow-service run submit: validate ≤1 `python-env` and ≤1 `r-env` per processor.

**Acceptance:** selecting two env layers is rejected (UI + API); multiple data layers allowed.

**Depends on:** M1. **Decisions:** D2.

---

## M9 — R env (parity)

**Goal:** `r-env` layers, mirroring python.

**Tasks**
- new `processor-build-r-layer` (`renv`/`pak` → library dir; emit `renv.lock` manifest).
- provisioner converter/wrapper: append to `R_LIBS_USER` (M3 mechanism).
- catalog/selector: R badge + version.

**Acceptance:** an R workflow consumes an `r-env` layer; provenance records `renv.lock` + R version.

**Depends on:** M2, M3. **Decisions:** D2.

---

## M10 — Reproduce + notebook snapshot

**Goal:** close the loop.

**Tasks**
- pennsieve-app: "re-run with this environment" on the RunDetail env dialog → pre-fill run
  launch pinned to the captured image digest + layer versions.
- pennsieve-app `JupyterSession/NotebookSession.vue`: "snapshot environment" / "save as
  layer"; freeze at session end; embed in `.ipynb` metadata.

**Acceptance:** a user re-runs a past run's exact environment; a notebook's environment is
captured at session end and embedded in the saved notebook.

**Depends on:** M4, M5, M6. **Decisions:** D4 (reproduce/provenance scope).

---

## Suggested sequencing

```
M1 ─▶ M2 ─▶ M3 (wiring)        ─┐
            M4 (capture) ─▶ M5 (app view) ─▶ M10 (reproduce)
M1 ─▶ M2 ─────────────────▶ M6 (self-service)
M1 ─────────────────────▶ M7 (data) , M8 (selection)
M2, M3 ─────────────────▶ M9 (R)
```

**First reviewable slice:** M1 + M2 (type model + manifest) — pure foundation, low risk,
unblocks everything. Then split: M3+M4+M5 (the provenance/usability spine) vs M6
(self-service) can proceed in parallel once M2 lands.

---

## Cross-repo summary

| Repo | Milestones |
|---|---|
| workflow-service | M1, M2, M4, M6, M8 |
| compute-node-aws-provisioner-v2 (`data-transfer`, `aslconverter`, `workflow-finalizer`, gateway) | M1, M2, M3, M4, M7, M9 |
| account-service | M1 (model mirror), M6 (build-trigger API) |
| processor-build-python-layer / new processor-build-r-layer | M2, M9 |
| pennsieve-app (`Analysis/`) | M5, M6, M8, M10 |
