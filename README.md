# Kedify build of KEDA HTTP Add-on

Main work branch from which releases are cut is [kedify-main](https://github.com/kedify/http-add-on/tree/kedify-main).

The main branch corresponds to the upstream KEDA HTTP Add-on [main](https://github.com/kedacore/http-add-on) branch.

### How to release Kedify HTTP Add-on

Whenever there is a new upstream KEDA HTTP Add-on release - https://github.com/kedacore/http-add-on/releases, we can trigger
Kedify HTTP Add-on release workflow to release Kedify build by running `tag_kedify_release` workflow.

This workflow will
* sync all commits from kedacore to kedify
* create `kedify-${upstream_release_tag}` branch to version kedify release on top of upstream release
* cherry-pick kedify patches on the top and tag with upstream tag suffixed by kedify counter

For example, when there is HTTP Add-on `v0.8.0` release, it will create `kedify-v0.8.0` branch in kedify/http-add-on, 
cherry-pick all between `main` and `kedify-main` on that branch and create tag `v0.8.0-0` to distinguish
kedify release tag from upstream release tag. At that point `kedify-release` at `kedify-main` branch will be triggered to build and push kedify/keda images for both 
1) regular builds as well as 
2) TODO: ~~FIPS compliant (hardened) builds.~~

If we need new kedify release without upstream release, we can add commits to the `kedify-v0.8.0`
release branch and tag as `kedify-v0.8.0-1`, then `kedify-v0.8.0-2`, ...
Next upstream kedacore/keda release `kedify-v0.8.1` will trigger creation of new `kedify-v0.8.1` 
branch with tag `kedify-v0.8.1-0`.

### Post release steps

Once KEDA images are published we might want to update default KEDA versions for Kedify:
- Update default version in the Dashboard, for example see https://github.com/kedify/dashboard/pull/447
- Update default version in the [dashboard-api-service]](https://github.com/kedify/dashboard-api-service/blob/main/internal/model/kedaconfig.go), for example see https://github.com/kedify/dashboard-api-service/pull/233
