# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

ARG DEBIAN_ARCH=amd64
# We not using scratch because we need to keep the basic image information
# from parent image
FROM k8s.gcr.io/build-image/debian-base-${DEBIAN_ARCH}:v2.1.3

ARG ARCH=amd64

# Fill out the labels
LABEL name="cinder-csi-plugin" \
      license="Apache Version 2.0" \
      maintainers="Kubernetes Authors" \
      description="Cinder CSI Plugin" \
      architecture=$ARCH \
      distribution-scope="public" \
      summary="Cinder CSI Plugin" \
      help="none"

ADD rootfs.tar /

CMD ["/bin/cinder-csi-plugin"]
