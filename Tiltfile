# Start the isolated cluster once with: bash hack/dev/run.sh up
# Each source change replaces the controller, then runs installed-controller E2E.
if os.getenv('OPTIPOD_CONSOLE_DEV') == '1':
    local_resource('console-database-forward', serve_cmd='bash hack/console/run.sh database-forward')
    local_resource('console-ui-forward', serve_cmd='bash hack/console/run.sh forward')
    local_resource('console-e2e',
        cmd='bash hack/console/run.sh reload && bash hack/console/run.sh test',
        deps=['internal/console', 'internal/consoleagent', 'internal/history', 'internal/report',
              'cmd/optipod-console', 'charts/optipod-console', 'hack/console',
              'test/e2e/console', 'go.mod', 'go.sum'],
        resource_deps=['console-database-forward', 'console-ui-forward'],
        labels=['validation'])
else:
    local_resource('optipod-e2e',
        cmd='bash hack/dev/run.sh reload && bash hack/dev/run.sh test "' + os.getenv('OPTIPOD_E2E_LABELS', 'smoke || parity') + '"',
        deps=['api', 'cmd', 'internal', 'config', 'go.mod', 'go.sum',
              'test/e2e/suite', 'test/e2e/framework', 'test/e2e/metricsfixture'],
        labels=['validation'])
