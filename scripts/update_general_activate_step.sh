#!/bin/bash

set -ex

# Configuration
STEP_NAME="bitrise-step-activate-gradle-features"
REPO_NAME="bitrise-steplib/$STEP_NAME"
PR_TITLE="feat: Release new CLI $BITRISE_GIT_TAG"

# Clone the repository
git clone "https://$GITHUB_TOKEN@github.com/$REPO_NAME"
cd $STEP_NAME

 # Check for existing PR with the same title
existing_pr=$(curl -s -H "Authorization: token $GITHUB_TOKEN" \
  "https://api.github.com/repos/$REPO_NAME/pulls?state=open" | jq -r ".[] | select(.title == \"$PR_TITLE\") | .html_url")

if [ -n "$existing_pr" ]; then
  echo "A pull request with this title already exists: $existing_pr"
  exit 0
fi

# Update the version in the file
SED_IN_PLACE_COMMAND=(-i)
if [[ "$OSTYPE" == "darwin"* ]]; then
  SED_IN_PLACE_COMMAND=(-i "")
fi

go get github.com/bitrise-io/bitrise-build-cache-cli@${BITRISE_GIT_TAG}
go mod tidy
go mod vendor

if [ -n "$(git status --porcelain)" ]; then
  git branch -D update-cli || true
  git checkout -b update-cli

  git add .
  git commit -m "feat: update CLI to release"
  git push -f origin update-cli


 # Create a pull request using GitHub API
  pr_response=$(curl -s -X POST -H "Authorization: token $GITHUB_TOKEN" \
    -d "{\"title\":\"$PR_TITLE\",\"body\":\"This PR updates the Bitrise Build Cache CLI.\",\"head\":\"update-cli\",\"base\":\"main\"}" \
    "https://api.github.com/repos/$REPO_NAME/pulls")

    pr_url=$(echo "$pr_response" | jq -r .html_url)
    envman add --key GENERAL_ACTIVATE_PR_URL --value "$pr_url"

    if [ "$pr_url" != "null" ]; then
      echo "Pull request created successfully: $pr_url"
    else
      echo "Failed to create pull request. Response: $pr_response"
      exit 1
    fi
else
  echo "No changes detected, skipping commit."
  exit 0
fi
