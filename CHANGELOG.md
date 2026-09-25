# Changelog

## [0.3.0](https://github.com/7K-Inari/inari-ext-argocd/compare/v0.2.0...v0.3.0) (2026-09-24)


### Features

* dial-mode HTTP command for the extension host proxy ([cd8946b](https://github.com/7K-Inari/inari-ext-argocd/commit/cd8946bfd31ee17e16be3be7696a0278f93e2c3c))
* **gateway:** send x-inari-extension-token gate token when configured ([fc4c539](https://github.com/7K-Inari/inari-ext-argocd/commit/fc4c5399b0ecc381376d352e10980ead496a50f6))
* **http:** INARI_COMPAT_API_VERSION override for pre-fix servers ([73a8da0](https://github.com/7K-Inari/inari-ext-argocd/commit/73a8da0d2fe31633a13c154fd8d24403ad9ddd81))


### Bug Fixes

* populate InvokeAction.Resource (agent resolves the Application from it) ([72a68ea](https://github.com/7K-Inari/inari-ext-argocd/commit/72a68ea35c8c8658f686de9dd50e3e19b9889fd9))
* rollback param as bare id (real agent contract); stub accepts id with revisionId fallback ([27730d0](https://github.com/7K-Inari/inari-ext-argocd/commit/27730d07965c6d9be070cd8bfa1aed53d5567b27))
* tunnel bare agent verbs (sync|refresh|rollback) instead of namespaced actions ([0de28be](https://github.com/7K-Inari/inari-ext-argocd/commit/0de28be1da0cc8e7c63011a84bf870354d7084f0))
* **ui:** correct SDK share name, shell bearer auth, richer cluster tab ([#13](https://github.com/7K-Inari/inari-ext-argocd/issues/13)) ([b3cfd64](https://github.com/7K-Inari/inari-ext-argocd/commit/b3cfd646acb998cfa8c95fac7f87831936698bf3))
* **ui:** tenant-scoped instances path (was hardcoded /api/v1/resources → 404) ([682964b](https://github.com/7K-Inari/inari-ext-argocd/commit/682964ba3f7738499627195f47a2b873da235e39))

## [0.2.0](https://github.com/7K-Inari/inari-ext-argocd/compare/v0.1.1...v0.2.0) (2026-08-21)


### Features

* add ArgoCD backend extension with fail-closed agent gateway tunnel ([9489dd3](https://github.com/7K-Inari/inari-ext-argocd/commit/9489dd3c7c14d34f2b20b42a0ef45588af5ff891))
* ArgoCD reference extension (backend plugin, UI remote, e2e) ([7a16d76](https://github.com/7K-Inari/inari-ext-argocd/commit/7a16d76490ff82f86340a110deaec3dd97f5b3c7))
* **ui:** add ArgoCD Module Federation remote with blueprint slots ([e6b1cd1](https://github.com/7K-Inari/inari-ext-argocd/commit/e6b1cd1fc91ede435900fb4ec1f46477fb70408d))


### Bug Fixes

* **ui:** copy only published file set when vendoring UI SDK build ([a9d2477](https://github.com/7K-Inari/inari-ext-argocd/commit/a9d2477cf7bdffa73e79c173b225f0b6bf17cfbd))

## [0.1.1](https://github.com/7K-Inari/inari-ext-argocd/compare/v0.1.0...v0.1.1) (2026-08-14)


### Bug Fixes

* **ci:** detect release-please merges made with merge commits ([a189391](https://github.com/7K-Inari/inari-ext-argocd/commit/a1893917f963299dc4c7a3745996f18ea15494df))
