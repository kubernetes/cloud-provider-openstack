#!/bin/bash

script_name="$0"
secret_namespace=default

usage() {
	echo "Usage: $script_name -n SECRET_NAME [-s SECRET_NAMESPACE] [-i OPENRC_FILE]"
	echo "  -n secret name (required)"
	echo "  -s secret namespace (optional, defaults to 'default')"
	echo "  -i input OpenRC file (optional, reads OS_* environment variables if not specified)"
}

die() {
	echo $1 >&2
	exit 1
}

encode() {
	if [[ -z "${!1}" ]]; then
		die "OpenRC variable $1 not set"
	fi
	echo -n "${!1}" | base64
}

extract_value() {
	read $1 < <(perl -n -e"/export $1=(.+)/ && print \$1" $input_file)
}

if [[ $# -lt 2 ]]; then
	usage
	exit 1
fi

while [[ -n "$1" ]]; do
	case "$1" in
		-n) secret_name=$2; shift ;;
		-s) secret_namespace=$2; shift ;;
		-i) input_file=$2; shift ;;
		-h) usage; exit 0 ;;
		*) usage; die "Unrecognized option $1"
	esac
	shift
done

if [[ -z "$secret_name" ]]; then
	die "Missing secret name"
fi

if [[ -n "$input_file" ]]; then
	extract_value OS_AUTH_URL
	extract_value OS_USERNAME
	extract_value OS_USER_ID
	extract_value OS_PROJECT_ID
	extract_value OS_PROJECT_NAME
	extract_value OS_PROJECT_DOMAIN_ID
	extract_value OS_PROJECT_DOMAIN_NAME
	extract_value OS_REGION_NAME
	echo "Please enter your OpenStack Password for project $OS_PROJECT_NAME as user $OS_USERNAME: " >&2 
	read -sr OS_PASSWORD
elif [[ -z $(env | grep OS_) ]]; then
	die 'No OS_* variables in environment'
fi

cat << EOF
apiVersion: v1
kind: Secret
metadata:
  name: $secret_name
  namespace: $secret_namespace
data:
  os-authURL: "$(encode OS_AUTH_URL)"
  os-userID: "$(encode OS_USER_ID)"
  os-userName: "$(encode OS_USERNAME)"
  os-password: "$(encode OS_PASSWORD)"
  os-projectID: "$(encode OS_PROJECT_ID)"
  os-projectName: "$(encode OS_PROJECT_NAME)"
  os-domainID: "$(encode OS_PROJECT_DOMAIN_ID)"
  os-domainName: "$(encode OS_PROJECT_DOMAIN_NAME)"
  os-region: "$(encode OS_REGION_NAME)"
EOF

