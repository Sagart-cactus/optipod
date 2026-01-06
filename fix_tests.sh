#!/bin/bash

# Fix all remaining instances of old engine initialization in the test file
sed -i.bak '
/engine := &Engine{/{
N
/dynamicClient: mockDynamic,/{
N
/}/{
s/engine := &Engine{\n\t\t\tdynamicClient: mockDynamic,\n\t\t}/engine := createMockEngineWithDynamicClient(mockDynamic)/
}
}
}
' internal/application/engine_test.go

# Remove old initialization blocks
sed -i.bak '
/Initialize the engine properly to avoid nil pointer dereference/{
N
N
N
N
d
}
' internal/application/engine_test.go

echo "Fixed engine test file"
