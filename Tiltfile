# Start the isolated cluster once with: bash hack/dev/run.sh up
# Each source change replaces the controller, then runs installed-controller E2E.
local_resource('optipod-e2e',
    cmd='bash hack/dev/run.sh reload && bash hack/dev/run.sh test "' + os.getenv('OPTIPOD_E2E_LABELS', 'smoke || parity') + '"',
    deps=['api', 'cmd', 'internal', 'config', 'go.mod', 'go.sum',
          'test/e2e/suite', 'test/e2e/framework', 'test/e2e/metricsfixture'],
    labels=['validation'])
