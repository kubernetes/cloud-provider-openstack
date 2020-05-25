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

2. Sign tag and push to upstream repo. You will need your GPG pass phrase. Check if you have a key using `gpg --list-secret-keys --keyid-format LONG`

```
$ git tag -s -m "Release for cloud-provider-openstack to support Kubernetes release x" vX.Y.Z
$ git push upstream vX.Y.Z
```
3. Build and push images to dockerhub. Provide `ARCHS` if needs to push only for specific types of architecture (for example `ARCHS='arm64 amd64'` or `ARCHS='amd64'`).
Don't need to provide `DOCKER_USERNAME` and `DOCKER_PASSWORD` if you already logged in.

```
GOOS=linux DOCKER_USERNAME=user DOCKER_PASSWORD=my_password REGISTRY=docker.io/k8scloudprovider VERSION=vX.Y.Z make upload-images
```

4. Update manifests with new release images, create a PR against release branch to update.

5. Create entry in GitHub
```
https://github.com/kubernetes/cloud-provider-openstack/releases
```
Click on "Draft a new release", fill out the tag, title and description field and click on "Publish release"

4. Sign the tarball with your key and upload vX.Y.Z.tar.gz.asc and vX.Y.Z.zip.asc

```
gpg --armor --detach-sign vX.Y.Z.tar.gz
gpg --armor --detach-sign vX.Y.Z.zip
```

6 Make the binaries, sign them and upload them

```
VERSION=vX.Y.Z make dist
cd _dist
find . -name "*.tar.gz" -exec gpg --armor --detach-sign {} \;
find . -name "*.zip" -exec gpg --armor --detach-sign {} \;
```
