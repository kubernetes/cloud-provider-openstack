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

1. Checkout the release branch.

    ```bash
    $ git fetch upstream
    $ git pull upstream master
    ```

2. Update the minor version with the expected version.

    Make changes in the `docs/manifests/tests/examples` directories using the
    `hack/bump-release.sh` script by running the following command:

    ```bash
    $ hack/bump-release.sh 28 29 0
    ```

    This will replace `1.28.x` with `1.29.0` strings in the
    `docs/manifests/tests/examples` directories. Ensure that you double-check the
    diff before committing the changes. Non-related changes must not be shipped.

3. Create a new pull request (PR) and make sure all CI checks have passed.

4. Once the PR is merged, make a tag and push it to the upstream repository.

    ```bash
    $ git checkout master
    $ git pull upstream master
    $ git tag vX.Y.Z
    $ git push upstream vX.Y.Z
    $ git checkout -b release-X.Y
    $ git push origin release-X.Y
    ```

    New [Docker images](https://console.cloud.google.com/gcr/images/k8s-staging-provider-os) will be built.

6. Make PR modifying [images.yaml](https://github.com/kubernetes/k8s.io/blob/main/registry.k8s.io/images/k8s-staging-provider-os/images.yaml) to promote gcr.io images to registry.k8s.io. The point is to copy the proper image sha256 hashes from the staging repository to the images.yaml.

    Use `hack/release-image-digests.sh` script and `hack/verify-image-digests.sh` to verify the digests before submitting the PR.

    ```bash
    $ ./hack/release-image-digests.sh ../k8s.io/registry.k8s.io/images/k8s-staging-provider-os/images.yaml vX.Y.Z
    ```

    Generate a PR with the updated `images.yaml` file. Make sure to review the changes and ensure that the correct images are being promoted.

7. Once images are promoted (takes about 30 minutes) create release notes using the "Generate release notes" button in the GitHub "New release" UI and publish the release.

8. Update the helm chart version with the expected version.

    Make changes in the `charts` directory using the
    `hack/bump-release.sh` script by running the following command:

    ```bash
    $ hack/bump-charts.sh 28 29 0
    ```

    This will replace `1.28.x`/`2.28.x` with `1.29.0`/`2.29.0` strings in the
    `docs/manifests/tests/examples` directories. Ensure that you double-check the
    diff before committing the changes. Non-related changes must not be shipped.

    Make a PR to bump the chart version in the `charts` directory. Once the PR is
    merged, the chart will be automatically published to the repository registry.

9. Update `kubernetes/test-infra` to add jobs for the new release branch in the [`config/jobs/kubernetes/cloud-provider-openstack`](https://github.com/kubernetes/test-infra/tree/master/config/jobs/kubernetes/cloud-provider-openstack) directory.

    This is generally as simple as copying the `release-master` file to `release-X.Y`, adding `--release-XY` suffixes to the job names and `testgrid-tab-name` annotations, and updating the branch specifiers.
