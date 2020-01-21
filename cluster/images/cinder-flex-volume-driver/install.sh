#!/bin/bash
# Copyright 2017 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

# gracefully handle the TERM signal sent when deleting the daemonset
trap 'exit' TERM

# WORKDIR /flexvolume
TMP_CONF='cinder.conf.tmp'
VOL_BINARY='cinder-flex-volume-driver'

VOL_BINARY_DIR='/mnt/binary'

# Check environment variables before any real actions.
for var in 'AUTH_URL' 'USERNAME' 'PASSWORD' 'TENANT_NAME' 'REGION' 'KEYRING';do
	if [ "${!var}" ];then
		echo "environment variable $var = ${!var}"
	else
		echo "environment variable $var is empty, exit..."
		exit 1
	fi
done

# Insert parameters.
sed -i s~_AUTH_URL_~${AUTH_URL:-}~g ${TMP_CONF}
sed -i s/_USERNAME_/${USERNAME:-}/g ${TMP_CONF}
sed -i s/_PASSWORD_/${PASSWORD:-}/g ${TMP_CONF}
sed -i s/_TENANT_NAME_/${TENANT_NAME:-}/g ${TMP_CONF}
sed -i s/_REGION_/${REGION:-}/g ${TMP_CONF}
sed -i s~_KEYRING_~${KEYRING:-}~g ${TMP_CONF}

# Move the temporary Cinder config into place.
CINDER_CONFIG_FIlE='/mnt/config/cinder.conf'
mv ${TMP_CONF} ${CINDER_CONFIG_FIlE}
mv ${VOL_BINARY} ${VOL_BINARY_DIR}

echo "Successfully installed flexvolume!" 

# this is a workaround to prevent the container from exiting 
# and k8s restarting the daemonset pod
while true; do sleep 1; done
