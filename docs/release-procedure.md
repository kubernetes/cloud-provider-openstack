# Release Procedure

Major versions of OpenStack Cloud Provider are done in sync with
[kubernetes/kubernetes](https://github.com/kubernetes/kubernetes).
Minor versions can be released intermittently for critical bug fixes.

## Preparing for a release

Note that while we use the terms *major* and *minor* here and below, these
actually correspond to SemVer *minor* and *patch* versions. This is discussed
in detail in [the Kubernetes documentation](https://github.com/kubernetes/sig-release/blob/master/release-engineering/versioning.md#kubernetes-release-versioning).

### Major releases (`X.Y.0`)

The following steps can be submitted as separate commits in a single PR or as
separate PRs:

1. Bump the version of the sidecar container images used in both the manifests
   and Helm Charts.

    You should pay particular attention to any major version bumps, since they
    may require additional changes to the manifests or charts.

    Example: https://github.com/kubernetes/cloud-provider-openstack/pull/3008

2. (Optional) Bump any major, non-kubernetes dependencies

    You may wish to bump the version of important dependencies like
    `github.com/gophercloud/gophercloud` before bumping the version of
    `k8s.io/kubernetes`.

2. Bump the version of `k8s.io/kubernetes` to the latest minor version.

    ```bash
    go get -u k8s.io/kubernetes@latest
    ```

    Note that this will frequently bring in a new Go version corresponding to
    the latest and greatest version. It will also automatically bump most of
    the other dependencies from `k8s.io` to the same version. However, you must
    manually bump the dependencies in the `replace` directive, once again using
    the same version as `k8s.io/kubernetes`. Once done, you can confirm that
    they are valid and that are none are missing by running `go list -m all`.
    You may also wish to ensure that none are unnecessary by temporarily
    deleting the `replace` directive and comparing the list of packages in the
    output with the list of packages in the replace directive.

    Example: https://github.com/kubernetes/cloud-provider-openstack/pull/3010

3. Bump remaining dependencies.

    Once again, pay close attention to any major version bumps of packages,
    ensuring API changes are accounted for.

### Minor releases (`X.Y.Z`, `Z` > 0)

The release process for a minor release is effectively the same as the release
process for major releases. However, you should only bump the *patch* version of
`k8s.io/kubernetes` and avoid bumping the *major* version of any other
dependency or sidecar container.

## Making a Release

> [!NOTE]
> This section only applies to releasing a new version of
> cloud-provider-openstack itself. If you are just updating the Helm Charts,
> refer to [Helm Charts](#helm-charts) below.

### Major releases (`X.Y.0`)

1. Checkout the `master` branch.

    ```bash
    $ git fetch upstream
    $ git checkout master
    $ git pull --rebase upstream master
    ```

1. Bump the release version.

    Run `hack/bump-release.py`, which detects the current branch automatically
    and updates the Helm chart versions (`charts/`) and all image references in
    `docs/`, `manifests/`, and `examples/`:

    ```bash
    $ uv run hack/bump-release.py
    ```

    Ensure that you double-check the diff before committing the changes.
    Non-related changes must not be shipped.

1. Update the k3s and kubernetes-test versions used in our tests with the expected version.

1. Create a new pull request (PR) and make sure all CI checks have passed.

1. Make a `vX.Y.0` release tag and push it to the upstream repository.

    ```bash
    $ git checkout master
    $ git pull upstream master
    $ git tag vX.Y.0
    $ git push upstream vX.Y.0
    ```

    This will kick the [`cloud-provider-openstack-push-images`
    job](https://prow.k8s.io/job-history/gs/kubernetes-ci-logs/logs/cloud-provider-openstack-push-images)
    and will result in new container images being pushed to [the staging
    area](https://console.cloud.google.com/artifacts/docker/k8s-staging-provider-os/us/gcr.io).

    Tags will also be automatically be created for any Helm Charts that have
    changed their version (i.e. `openstack-cloud-controller-manager-X.Y.Z`,
    `openstack-cinder-csi-X.Y.Z`, and `openstack-manila-csi-X.Y.Z`).

1. Make a `release-X.Y` release branch and push it to the upstream repository

    ```bash
    $ git checkout -b release-X.Y
    $ git push origin release-X.Y
    ```

1. Reset the `version` field of the Helm Charts to `2.{X+1}.0-dev`

    Any bugfixes for the Helm Charts must be backported to the `release-*`
    stable branches and released from there.

1. Make PR modifying
   [images.yaml](https://github.com/kubernetes/k8s.io/blob/main/registry.k8s.io/images/k8s-staging-provider-os/images.yaml)
   to promote staging images to registry.k8s.io. The point is to copy the proper image
   sha256 hashes from the staging repository to the `images.yaml`.

    Use `hack/release-image-digests.sh` script and `hack/verify-image-digests.sh` to
    verify the digests before submitting the PR.

    ```bash
    $ ./hack/release-image-digests.sh ../k8s.io/registry.k8s.io/images/k8s-staging-provider-os/images.yaml vX.Y.Z
    ```

    Generate a PR with the updated `images.yaml` file. Make sure to review the changes
    and ensure that the correct images are being promoted.

1. Once images are promoted (takes about 30 minutes) create release notes using the
   "Generate release notes" button in the GitHub "New release" UI and publish the
   release.

1. Update `kubernetes/test-infra` to add jobs for the new release branch in the
   [`config/jobs/kubernetes/cloud-provider-openstack`](https://github.com/kubernetes/test-infra/tree/master/config/jobs/kubernetes/cloud-provider-openstack)
   directory.

    This is generally as simple as copying the `release-master` file to `release-X.Y`,
    adding `--release-XY` suffixes to the job names and `testgrid-tab-name` annotations,
    and updating the branch specifiers.

### Minor releases (`X.Y.Z`, `Z` > 0)

The release process for a minor release is effectively the same as the release
process for major releases but with the following changes:

1. You must always bump the Helm Chart `appVersion` and `version` fields.

1. It is not necessary to create a new branch or add new jobs.

## Helm Charts

Chart versions on `master` use a `-dev` pre-release suffix (e.g.
`2.37.0-dev`) and are **not** bumped for individual PRs. Version bumps only
happen at release time (see [Major releases](#major-releases-xy0) above).

On `release-*` branches the chart version (`version`) **should** be bumped for
every backported change to a chart(s) including changes to `appVersion`. This
can be done in the backport PR or later, via a separate PR. The
`hack/bump-release.py` will automatically bump the correct chart if there have
been any changes.

Once version change is merged, tags are automatically created for any charts
whose version changed (i.e. `openstack-cloud-controller-manager-X.Y.Z`,
`openstack-cinder-csi-X.Y.Z`, and `openstack-manila-csi-X.Y.Z`).
