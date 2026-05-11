package host

import (
	"queuebuzz/tests/e2e/setup"
	"queuebuzz/tests/e2e/testenv"
	"testing"
)

func Suite(t *testing.T) *setup.TestSuite {
	mongoURI, redisURL, mailpitURL := testenv.Setup()
	return setup.NewTestSuite(t, mongoURI, redisURL, mailpitURL, nil)
}
