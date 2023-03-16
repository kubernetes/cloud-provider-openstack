# Release Procedure

Cloud Provider OpenStack Release is done in sync with kubernetes/kubernetes, Minor versions can be released intermittently for critical bug fixes.

## Before Release

Update cloud-provider-openstack to kubernetes/kubernetes latest release. Make Sure all CI check passed.

## Making a Release

1. Checkout the release branch

```
$ git fetch upstream
$ git checkout -b release-X.Y upstream/release-X.Y
```

2. Update manifests with new release images, create a PR against release branch to update.

3. Make tag and push to upstream repo.

```
$ git tag vX.Y.Z
$ git push upstream vX.Y.Z
```

4. [Github Actions](https://github.com/kubernetes/cloud-provider-openstack/actions/workflows/release-cpo.yaml) will make [new draft release](https://github.com/kubernetes/cloud-provider-openstack/releases) to repository.
Cloudbuild should build new images to gcr.io/k8s-staging-provider-os. 

5. Make PR https://github.com/kubernetes/k8s.io/blob/main/k8s.gcr.io/images/k8s-staging-provider-os/images.yaml to promote gcr.io images to registry.k8s.io.

6. Make release notes and publish the release after the new images are published.
