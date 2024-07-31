# Kedify build of KEDA HTTP Add-on

Main work branch from which releases are cut is [kedify-main](https://github.com/kedify/http-add-on/tree/kedify-main).

The main branch corresponds to the upstream KEDA HTTP Add-on [main](https://github.com/kedacore/http-add-on) branch.

### How to release Kedify HTTP Add-on

Whenever there is a new upstream KEDA HTTP Add-on release - https://github.com/kedacore/http-add-on/releases, we can trigger
`Kedify release trigger` workflow to release Kedify build of http-add-on by running `tag_kedify_release` workflow.
For cherry-picking latest changes to already existing downstream release branches, there is
`Kedify cherry-pick trigger` workflow to release custom kedify patches also through `tag_kedify_release` workflow
under the hood.

#### [Kedify release trigger](https://github.com/kedify/http-add-on/actions/workflows/kedify-release-trigger.yml)
* sync all commits from `kedacore` to `kedify` org for `main` and `release*` branches
* rebase `kedify-main` on newly synced `main`
* create `kedify-${upstream_release_tag}` branch to version kedify release on top of upstream release
* cherry-pick kedify patches from `kedify-main` on the top and tag with upstream tag suffixed by kedify counter

For example, when there is HTTP Add-on `v0.8.0` release, it will create `kedify-v0.8.0` branch in kedify/http-add-on, 
cherry-pick all between `main` and `kedify-main` on that branch and create tag `v0.8.0-0` to distinguish
kedify release tag from upstream release tag. At that point `kedify-release` at `kedify-main` branch will be triggered to build and push kedify/keda images for both 
1) regular builds as well as 
2) FIPS compliant (hardened) builds.

#### [Kedify cherry-pick trigger](https://github.com/kedify/http-add-on/actions/workflows/kedify-patch-cherry-pick-trigger.yml)
* get top commits from `kedify-main` missing in a downstream release branch
* cherry-pick the range to the release branch
* create new tag with incremented kedify counter

New kedify patches get merged to `kedify-main` branch. In order to get these to some release, they need to be
cherry-picked to a certain downstream release branch and tagged as `v0.8.0-1`, then `v0.8.0-2`, ...

### Post release steps

Once KEDA images are published we might want to update default KEDA versions for Kedify:
- Update default version in the Dashboard, for example see https://github.com/kedify/dashboard/pull/447
- Update default version in the [dashboard-api-service]](https://github.com/kedify/dashboard-api-service/blob/main/internal/model/kedaconfig.go), for example see https://github.com/kedify/dashboard-api-service/pull/233
