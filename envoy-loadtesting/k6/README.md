# Running the k6 test scenario

In order to run the k6 test scenario, you have to run the `deploy-test.sh` script when your kubeconfig is pointing to a cluster running the k6-operator. Currently, Adidas is running the operator on its `alba-seu01` cluster and has the `gs-k6-operator` namespace dedicated to the deployment of TestRun CRs. This is important as running k6 tests require several kyverno PolicyExceptions to allow the generated pods to work as expected.

This folder contains 4 files (not counting this one):

- **test-scenario.js**: this is the JS test scenario file.
- **testrun.yaml**: this file contains the TestRun CR k6 needs to run.
- **configmap.yaml**: this file contains the configMap with the JS test scenario file as data.
- **deploy-test.sh**: this script generates the configmap.yaml file from the test-scenario.js file and then deploys both the configmap and the testrun CR to the current context's kubernetes cluster.

For more information concerning how to run k6 tests, please refer to the [official k6 documentation](https://grafana.com/docs/k6/latest/set-up/set-up-distributed-k6/).
