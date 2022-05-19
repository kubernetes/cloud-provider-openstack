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

2. Make tag and push to upstream repo.

```
$ git tag -m "Release for cloud-provider-openstack to support Kubernetes release x" vX.Y.Z
$ git push upstream vX.Y.Z
```

3. Github Actions will make the new docker images and make new draft release to repository.

4. Update manifests with new release images, create a PR against release branch to update.

5. Make release notes and publish the release.
