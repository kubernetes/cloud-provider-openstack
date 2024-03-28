# Release Procedure

The Cloud Provider OpenStack Release is done in sync with
kubernetes/kubernetes. Minor versions can be released intermittently for
critical bug fixes.

## Making a Release

1. Checkout the release branch.

    ```bash
    $ git fetch upstream
    $ git checkout -b my-release upstream/release-X.Y
    ```

2. Update the minor version with the expected version.

    Make changes in the `docs/manifests/tests/examples` directories using the
    `hack/bump_release.sh` script by running the following command:

    ```bash
    $ hack/bump-release.sh 28 29 0
    ```

    This will replace `1.28.x`/`2.28.x` with `1.29.0`/`2.29.0` strings in the
    `docs/manifests/tests/examples` directories. Ensure that you double-check the
    diff before committing the changes. Non-related changes must not be shipped.

3. Create a new pull request (PR) and make sure all CI checks have passed.

4. Once the PR is merged, make a tag and push it to the upstream repository.

    ```bash
    $ git checkout -b release-X.Y upstream/release-X.Y
    $ git pull upstream release-X.Y --tags
    $ git tag -m "Release for cloud-provider-openstack to support Kubernetes release x" vX.Y.Z
    $ git push upstream vX.Y.Z
    ```

5. [Github Actions](https://github.com/kubernetes/cloud-provider-openstack/actions/workflows/release-cpo.yaml) will create new [Docker images](https://console.cloud.google.com/gcr/images/k8s-staging-provider-os) and generate a [new draft release](https://github.com/kubernetes/cloud-provider-openstack/releases) in the repository.

6. Make PR modifying [images.yaml](https://github.com/kubernetes/k8s.io/blob/main/registry.k8s.io/images/k8s-staging-provider-os/images.yaml) to promote gcr.io images to registry.k8s.io. The point is to copy the proper image sha256 hashes from the staging repository to the images.yaml.

7. Once images are promoted create release notes using the "Generate release notes" button in the GitHub "New release" UI and publish the release.
