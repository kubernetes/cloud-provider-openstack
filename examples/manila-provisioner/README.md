## Preconditions

Generate Secrets manifest:
```bash
$ ./generate-secrets.sh -n my-manila-secrets | ./filter-secrets.sh > secrets.yaml
$ kubectl create -f secrets.yaml
```

You can run `$ ./generate-secrets.sh -h` for further details.

Set the Secret name in Storage Class manifest in `{demo}/user-deploy/sc.yaml`

## Running the demos
Run `{demo}/base-deploy.sh` and `{demo}/user-deploy/demo-deploy.sh`
For tearing down demos, run `{demo}/user-deploy/demo-teardown.sh` and `{demo}/base-teardown.sh`
