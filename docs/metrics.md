# Metrics documentation

This documentation is a reflection of the current state of the exposed metrics of the openstack-cloud-controller-manager
(OCCM) binary.

Any contribution to improving this documentation or adding sample usages will be appreciated.

<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Metrics for openstack-cloud-controller-manager](#metrics-for-openstack-cloud-controller-manager)
  - [OpenStack API calls](#openstack-api-calls)
  - [OpenStack cloud controller manager reconciliation](#openstack-cloud-controller-manager-reconciliation)
  - [Additional metrics](#additional-metrics)
  - [Useful metric queries](#useful-metric-queries)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

## Metrics for openstack-cloud-controller-manager

The openstack-cloud-controller-manager (OCCM) exposes metrics on https://localhost:10258/metrics.

For local testing, add the following parameters when running the OCCM.
```
openstack-cloud-controller-manager
  --authorization-always-allow-paths="/metrics"
  --secure-port=10258
```

### Exposing metrics to prometheus operator

Default setup assumes that cloud-controller-manager is located in master.
Please note that firewall between prometheus (usually normal node) and master port 10258 needs to be open.
After that you can just apply following manifests and you should be able to see statistics in prometheus soon.

```yaml
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: system:cloud-controller-manager:auth-delegate
subjects:
- kind: User
  name: system:serviceaccount:kube-system:cloud-controller-manager
  apiGroup: rbac.authorization.k8s.io
roleRef:
  kind: ClusterRole
  name: system:auth-delegator
  apiGroup: rbac.authorization.k8s.io

---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  labels:
    k8s-app: openstack-cloud-controller-manager
  name: openstack-cloud-controller-manager
  namespace: monitoring
spec:
  endpoints:
  - bearerTokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token
    interval: 30s
    port: http
    scheme: https
    tlsConfig:
      insecureSkipVerify: true
  jobLabel: component
  namespaceSelector:
    matchNames:
    - kube-system
  selector:
    matchLabels:
      k8s-app: openstack-cloud-controller-manager

---
apiVersion: v1
kind: Service
metadata:
  labels:
    k8s-app: openstack-cloud-controller-manager
  name: openstack-cloud-controller-manager
  namespace: kube-system
spec:
  ports:
  - name: http
    port: 10258
    protocol: TCP
  selector:
    k8s-app: openstack-cloud-controller-manager

```


### OpenStack API calls

|Metric name|Metric type|Labels/tags|Status|
|-----------|-----------|-----------|------|
|openstack_api_request_duration_seconds|Histogram|`request`=<api_request>|ALPHA|
|openstack_api_requests_total|Counter|`request`=<api_request>|ALPHA|
|openstack_api_request_errors_total|Counter|`request`=<api_request>|ALPHA|

The `request` label indicates the API call.
Possible request values:
* `flavor_get`
* `floating_ip_create`
* `floating_ip_delete`
* `floating_ip_list`
* `floating_ip_update`
* `loadbalancer_create`
* `loadbalancer_delete`
* `loadbalancer_get`
* `loadbalancer_healthmonitor_create`
* `loadbalancer_healthmonitor_delete`
* `loadbalancer_list`
* `loadbalancer_listener_create`
* `loadbalancer_listener_delete`
* `loadbalancer_listener_list`
* `loadbalancer_listener_update`
* `loadbalancer_member_create`
* `loadbalancer_member_delete`
* `loadbalancer_member_list`
* `loadbalancer_pool_create`
* `loadbalancer_pool_delete`
* `loadbalancer_pool_list`
* `loadbalancer_update`
* `network_extension_list`
* `network_list`
* `port_get`
* `port_list`
* `port_tag_add`
* `port_tag_delete`
* `port_update`
* `router_get`
* `router_update`
* `secret_create`
* `secret_delete`
* `secret_list`
* `security_group_create`
* `security_group_delete`
* `security_group_rule_create`
* `security_group_rule_delete`
* `security_group_rule_list`
* `server_get`
* `server_list`
* `server_os_interface_list`
* `subnet_get`
* `subnet_list`
* `version_list`

The metric output is similar to this example:
```
# HELP openstack_api_request_duration_seconds [ALPHA] Latency of an OpenStack API call
# TYPE openstack_api_request_duration_seconds histogram
openstack_api_request_duration_seconds_bucket{request="floating_ip_create",le="0.005"} 0
openstack_api_request_duration_seconds_bucket{request="floating_ip_create",le="0.01"} 0
openstack_api_request_duration_seconds_bucket{request="floating_ip_create",le="0.025"} 0
openstack_api_request_duration_seconds_bucket{request="floating_ip_create",le="0.05"} 0
openstack_api_request_duration_seconds_bucket{request="floating_ip_create",le="0.1"} 0
openstack_api_request_duration_seconds_bucket{request="floating_ip_create",le="0.25"} 0
openstack_api_request_duration_seconds_bucket{request="floating_ip_create",le="0.5"} 0
openstack_api_request_duration_seconds_bucket{request="floating_ip_create",le="1"} 0
openstack_api_request_duration_seconds_bucket{request="floating_ip_create",le="2.5"} 0
openstack_api_request_duration_seconds_bucket{request="floating_ip_create",le="5"} 0
openstack_api_request_duration_seconds_bucket{request="floating_ip_create",le="10"} 0
openstack_api_request_duration_seconds_bucket{request="floating_ip_create",le="+Inf"} 5
openstack_api_request_duration_seconds_sum{request="floating_ip_create"} 92.58063477700001
openstack_api_request_duration_seconds_count{request="floating_ip_create"} 5

# HELP openstack_api_requests_errors_total [ALPHA] Total number of errors for an OpenStack API call
# TYPE openstack_api_requests_errors_total counter
openstack_api_requests_total{request="loadbalancer_delete"} 1

# HELP openstack_api_requests_total [ALPHA] Total number of OpenStack API calls
# TYPE openstack_api_requests_total counter
openstack_api_requests_total{request="loadbalancer_create"} 6
```


### OpenStack cloud controller manager reconciliation

|Metric name|Metric type|Labels/tags|Status|
|-----------|-----------|-----------|------|
|cloudprovider_openstack_reconcile_duration_seconds|Histogram|`operation`=<reconciliation_operation>|ALPHA|
|cloudprovider_openstack_reconcile_total|Counter|`operation`=<reconciliation_operation>|ALPHA|
|cloudprovider_openstack_reconcile_errors_total|Counter|`operation`=<reconciliation_operation>|ALPHA|

The "operation" label indicates the reconciliation operation.
Possible operation values:
* `loadbalancer_delete`
* `loadbalancer_ensure`
* `loadbalancer_update`

The metric output is similar to this example:
```
# HELP cloudprovider_openstack_reconcile_duration_seconds [ALPHA] Time taken by various parts of OpenStack cloud controller manager reconciliation loops
# TYPE cloudprovider_openstack_reconcile_duration_seconds histogram
cloudprovider_openstack_reconcile_duration_seconds_bucket{operation="loadbalancer_delete",le="0.01"} 0
cloudprovider_openstack_reconcile_duration_seconds_bucket{operation="loadbalancer_delete",le="0.05"} 0
cloudprovider_openstack_reconcile_duration_seconds_bucket{operation="loadbalancer_delete",le="0.1"} 0
cloudprovider_openstack_reconcile_duration_seconds_bucket{operation="loadbalancer_delete",le="0.5"} 0
cloudprovider_openstack_reconcile_duration_seconds_bucket{operation="loadbalancer_delete",le="1"} 0
cloudprovider_openstack_reconcile_duration_seconds_bucket{operation="loadbalancer_delete",le="2.5"} 0
cloudprovider_openstack_reconcile_duration_seconds_bucket{operation="loadbalancer_delete",le="5"} 0
cloudprovider_openstack_reconcile_duration_seconds_bucket{operation="loadbalancer_delete",le="7.5"} 0
cloudprovider_openstack_reconcile_duration_seconds_bucket{operation="loadbalancer_delete",le="10"} 0
cloudprovider_openstack_reconcile_duration_seconds_bucket{operation="loadbalancer_delete",le="12.5"} 0
cloudprovider_openstack_reconcile_duration_seconds_bucket{operation="loadbalancer_delete",le="15"} 0
cloudprovider_openstack_reconcile_duration_seconds_bucket{operation="loadbalancer_delete",le="17.5"} 0
cloudprovider_openstack_reconcile_duration_seconds_bucket{operation="loadbalancer_delete",le="20"} 0
cloudprovider_openstack_reconcile_duration_seconds_bucket{operation="loadbalancer_delete",le="22.5"} 0
cloudprovider_openstack_reconcile_duration_seconds_bucket{operation="loadbalancer_delete",le="25"} 0
cloudprovider_openstack_reconcile_duration_seconds_bucket{operation="loadbalancer_delete",le="27.5"} 0
cloudprovider_openstack_reconcile_duration_seconds_bucket{operation="loadbalancer_delete",le="30"} 0
cloudprovider_openstack_reconcile_duration_seconds_bucket{operation="loadbalancer_delete",le="50"} 0
cloudprovider_openstack_reconcile_duration_seconds_bucket{operation="loadbalancer_delete",le="75"} 6
cloudprovider_openstack_reconcile_duration_seconds_bucket{operation="loadbalancer_delete",le="100"} 6
cloudprovider_openstack_reconcile_duration_seconds_bucket{operation="loadbalancer_delete",le="1000"} 6
cloudprovider_openstack_reconcile_duration_seconds_bucket{operation="loadbalancer_delete",le="+Inf"} 6
cloudprovider_openstack_reconcile_duration_seconds_sum{operation="loadbalancer_delete"} 378.40250376500006
cloudprovider_openstack_reconcile_duration_seconds_count{operation="loadbalancer_delete"} 6

# HELP cloudprovider_openstack_reconcile_errors_total [ALPHA] Total number of OpenStack cloud controller manager reconciliation errors
# TYPE cloudprovider_openstack_reconcile_errors_total counter
cloudprovider_openstack_reconcile_errors_total{operation="loadbalancer_ensure"} 1
cloudprovider_openstack_reconcile_errors_total{operation="loadbalancer_update"} 2

# HELP cloudprovider_openstack_reconcile_total [ALPHA] Total number of OpenStack cloud controller manager reconciliations
# TYPE cloudprovider_openstack_reconcile_total counter
cloudprovider_openstack_reconcile_total{operation="loadbalancer_delete"} 6
cloudprovider_openstack_reconcile_total{operation="loadbalancer_ensure"} 14
cloudprovider_openstack_reconcile_total{operation="loadbalancer_update"} 2
```

### Additional metrics

In addition to the previous metrics, the exporter exposes the following metrics:
```
# HELP apiserver_audit_event_total [ALPHA] Counter of audit events generated and sent to the audit backend.
# TYPE apiserver_audit_event_total counter
# HELP apiserver_audit_requests_rejected_total [ALPHA] Counter of apiserver requests rejected due to an error in audit logging backend.
# TYPE apiserver_audit_requests_rejected_total counter
# HELP apiserver_client_certificate_expiration_seconds [ALPHA] Distribution of the remaining lifetime on the certificate used to authenticate a request.
# TYPE apiserver_client_certificate_expiration_seconds histogram
# HELP apiserver_envelope_encryption_dek_cache_fill_percent [ALPHA] Percent of the cache slots currently occupied by cached DEKs.
# TYPE apiserver_envelope_encryption_dek_cache_fill_percent gauge
# HELP apiserver_storage_data_key_generation_duration_seconds [ALPHA] Latencies in seconds of data encryption key(DEK) generation operations.
# TYPE apiserver_storage_data_key_generation_duration_seconds histogram
# HELP apiserver_storage_data_key_generation_failures_total [ALPHA] Total number of failed data encryption key(DEK) generation operations.
# TYPE apiserver_storage_data_key_generation_failures_total counter
# HELP apiserver_storage_envelope_transformation_cache_misses_total [ALPHA] Total number of cache misses while accessing key decryption key(KEK).
# TYPE apiserver_storage_envelope_transformation_cache_misses_total counter
# HELP authenticated_user_requests [ALPHA] Counter of authenticated requests broken out by username.
# TYPE authenticated_user_requests counter
# HELP authentication_attempts [ALPHA] Counter of authenticated attempts.
# TYPE authentication_attempts counter
# HELP authentication_duration_seconds [ALPHA] Authentication duration in seconds broken out by result.
# TYPE authentication_duration_seconds histogram
# HELP go_gc_duration_seconds A summary of the pause duration of garbage collection cycles.
# TYPE go_gc_duration_seconds summary
# HELP go_goroutines Number of goroutines that currently exist.
# TYPE go_goroutines gauge
# HELP go_info Information about the Go environment.
# TYPE go_info gauge
# HELP go_memstats_alloc_bytes Number of bytes allocated and still in use.
# TYPE go_memstats_alloc_bytes gauge
# HELP go_memstats_alloc_bytes_total Total number of bytes allocated, even if freed.
# TYPE go_memstats_alloc_bytes_total counter
# HELP go_memstats_buck_hash_sys_bytes Number of bytes used by the profiling bucket hash table.
# TYPE go_memstats_buck_hash_sys_bytes gauge
# HELP go_memstats_frees_total Total number of frees.
# TYPE go_memstats_frees_total counter
# HELP go_memstats_gc_cpu_fraction The fraction of this program's available CPU time used by the GC since the program started.
# TYPE go_memstats_gc_cpu_fraction gauge
# HELP go_memstats_gc_sys_bytes Number of bytes used for garbage collection system metadata.
# TYPE go_memstats_gc_sys_bytes gauge
# HELP go_memstats_heap_alloc_bytes Number of heap bytes allocated and still in use.
# TYPE go_memstats_heap_alloc_bytes gauge
# HELP go_memstats_heap_idle_bytes Number of heap bytes waiting to be used.
# TYPE go_memstats_heap_idle_bytes gauge
# HELP go_memstats_heap_inuse_bytes Number of heap bytes that are in use.
# TYPE go_memstats_heap_inuse_bytes gauge
# HELP go_memstats_heap_objects Number of allocated objects.
# TYPE go_memstats_heap_objects gauge
# HELP go_memstats_heap_released_bytes Number of heap bytes released to OS.
# TYPE go_memstats_heap_released_bytes gauge
# HELP go_memstats_heap_sys_bytes Number of heap bytes obtained from system.
# TYPE go_memstats_heap_sys_bytes gauge
# HELP go_memstats_last_gc_time_seconds Number of seconds since 1970 of last garbage collection.
# TYPE go_memstats_last_gc_time_seconds gauge
# HELP go_memstats_lookups_total Total number of pointer lookups.
# TYPE go_memstats_lookups_total counter
# HELP go_memstats_mallocs_total Total number of mallocs.
# TYPE go_memstats_mallocs_total counter
# HELP go_memstats_mcache_inuse_bytes Number of bytes in use by mcache structures.
# TYPE go_memstats_mcache_inuse_bytes gauge
# HELP go_memstats_mcache_sys_bytes Number of bytes used for mcache structures obtained from system.
# TYPE go_memstats_mcache_sys_bytes gauge
# HELP go_memstats_mspan_inuse_bytes Number of bytes in use by mspan structures.
# TYPE go_memstats_mspan_inuse_bytes gauge
# HELP go_memstats_mspan_sys_bytes Number of bytes used for mspan structures obtained from system.
# TYPE go_memstats_mspan_sys_bytes gauge
# HELP go_memstats_next_gc_bytes Number of heap bytes when next garbage collection will take place.
# TYPE go_memstats_next_gc_bytes gauge
# HELP go_memstats_other_sys_bytes Number of bytes used for other system allocations.
# TYPE go_memstats_other_sys_bytes gauge
# HELP go_memstats_stack_inuse_bytes Number of bytes in use by the stack allocator.
# TYPE go_memstats_stack_inuse_bytes gauge
# HELP go_memstats_stack_sys_bytes Number of bytes obtained from system for stack allocator.
# TYPE go_memstats_stack_sys_bytes gauge
# HELP go_memstats_sys_bytes Number of bytes obtained from system.
# TYPE go_memstats_sys_bytes gauge
# HELP go_threads Number of OS threads created.
# TYPE go_threads gauge
# HELP kubernetes_build_info [ALPHA] A metric with a constant '1' value labeled by major, minor, git version, git commit, git tree state, build date, Go version, and compiler from which Kubernetes was built, and platform on which it is running.
# TYPE kubernetes_build_info gauge
# HELP process_cpu_seconds_total Total user and system CPU time spent in seconds.
# TYPE process_cpu_seconds_total counter
# HELP process_max_fds Maximum number of open file descriptors.
# TYPE process_max_fds gauge
# HELP process_open_fds Number of open file descriptors.
# TYPE process_open_fds gauge
# HELP process_resident_memory_bytes Resident memory size in bytes.
# TYPE process_resident_memory_bytes gauge
# HELP process_start_time_seconds Start time of the process since unix epoch in seconds.
# TYPE process_start_time_seconds gauge
# HELP process_virtual_memory_bytes Virtual memory size in bytes.
# TYPE process_virtual_memory_bytes gauge
# HELP process_virtual_memory_max_bytes Maximum amount of virtual memory available in bytes.
# TYPE process_virtual_memory_max_bytes gauge
# HELP rest_client_exec_plugin_certificate_rotation_age [ALPHA] Histogram of the number of seconds the last auth exec plugin client certificate lived before being rotated. If auth exec plugin client certificates are unused, histogram will contain no data.
# TYPE rest_client_exec_plugin_certificate_rotation_age histogram
# HELP rest_client_exec_plugin_ttl_seconds [ALPHA] Gauge of the shortest TTL (time-to-live) of the client certificate(s) managed by the auth exec plugin. The value is in seconds until certificate expiry (negative if already expired). If auth exec plugins are unused or manage no TLS certificates, the value will be +INF.
# TYPE rest_client_exec_plugin_ttl_seconds gauge
# HELP rest_client_request_duration_seconds [ALPHA] Request latency in seconds. Broken down by verb and URL.
# TYPE rest_client_request_duration_seconds histogram
# HELP rest_client_requests_total [ALPHA] Number of HTTP requests, partitioned by status code, method, and host.
# TYPE rest_client_requests_total counter
# HELP service_controller_rate_limiter_use [ALPHA] A metric measuring the saturation of the rate limiter for service_controller
# TYPE service_controller_rate_limiter_use gauge
# HELP workqueue_adds_total [ALPHA] Total number of adds handled by workqueue
# TYPE workqueue_adds_total counter
# HELP workqueue_depth [ALPHA] Current depth of workqueue
# TYPE workqueue_depth gauge
# HELP workqueue_longest_running_processor_seconds [ALPHA] How many seconds has the longest running processor for workqueue been running.
# TYPE workqueue_longest_running_processor_seconds gauge
# HELP workqueue_queue_duration_seconds [ALPHA] How long in seconds an item stays in workqueue before being requested.
# TYPE workqueue_queue_duration_seconds histogram
# HELP workqueue_retries_total [ALPHA] Total number of retries handled by workqueue
# TYPE workqueue_retries_total counter
# HELP workqueue_unfinished_work_seconds [ALPHA] How many seconds of work has done that is in progress and hasn't been observed by work_duration. Large values indicate stuck threads. One can deduce the number of stuck threads by observing the rate at which this increases.
# TYPE workqueue_unfinished_work_seconds gauge
# HELP workqueue_work_duration_seconds [ALPHA] How long in seconds processing an item from workqueue takes.
# TYPE workqueue_work_duration_seconds histogram
```

### Useful metric queries

Some useful PromQL queries:
* Failing OpenStack API calls: \
  `rate(openstack_api_request_errors_total[5m]) > 0.01`
* OpenStack API requests take longer than 15 seconds: \
  `rate(openstack_api_request_duration_seconds_sum[5m]) / rate(openstack_api_request_duration_seconds_count[5m]) > 15`
* Too high amount of OpenStack API calls: \
  `(delta(openstack_api_requests_total[5m]))/5 > 20`
* Increased reconciliation errors: \
  `rate(cloudprovider_openstack_reconcile_errors_total[5m]) > 0`
* Reconciliation takes longer than 10 minute: \
  `rate(cloudprovider_openstack_reconcile_duration_seconds_sum[5m]) / rate(cloudprovider_openstack_reconcile_duration_seconds_count[5m]) > 600`

Here is an example of a Prometheus rule that can be used to alert on failed reconciliation loops.
```
groups:
- name: openstack
  - alert: OpenStackReconcileFailed
    expr: rate(cloudprovider_openstack_reconcile_errors_total[5m]) > 0
    for: 10m
    labels:
      severity: warning
    annotations:
      summary: "Pod {{$labels.namespace}}/{{$labels.pod}} has increased reconciliation errors in the last 10m."
```
