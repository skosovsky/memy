package main

import "testing"

func TestOfflineLifecycle(t *testing.T) {
	// Arrange: run provisions an isolated temporary SQLite database.
	// Act.
	operationErr := run()
	// Assert: run verifies canonical revision, exact recall and pending purge.
	if operationErr != nil {
		t.Fatal(operationErr)
	}
}
