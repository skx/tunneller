# pushes trigger the testsuite
workflow "Push Event" {
  on = "push"
  resolves = ["Test"]
}

# pull-requests trigger the testsuite
workflow "Pull Request" {
  on = "pull_request"
  resolves = ["Test"]
}

# releases trigger new binary artifacts
workflow "Handle Release" {
  on = "release"
  resolves = ["Upload"]
}

##
## The actions
##


##
## Run the test-cases, via .github/run-tests.sh
##
action "Test" {
  uses = "skx/github-action-tester@master"
}

##
## Build the binaries, via .github/build, then upload them.
##
action "Upload" {
  uses = "skx/github-action-publish-binaries@master"
  args = "tunneller-*"
  secrets = ["GITHUB_TOKEN"]
}
