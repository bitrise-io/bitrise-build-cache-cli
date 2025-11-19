#!/bin/bash

set -ex

# Configuration
# STEP_NAME="bitrise-step-activate-gradle-remote-cache"
# UPDATE_SCRIPT_PATH="./scripts/update_activate_gradle_remote_cache.sh"

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

"$UPDATE_SCRIPT_PATH"

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

    if [ "$pr_url" != "null" ]; then
      envman add --key "SLACK_MESSAGE" --value "${STEP_NAME} update PR is ready! :tada: :rocket: :bitrise:\n\nCheck PR here: $pr_url"
      envman add --key "SLACK_EMOJI" --value ":gradle:"
      envman add --key "SLACK_COLOR" --value "#08a045"
      echo "Pull request created successfully: $pr_url"
    else
      envman add --key "SLACK_MESSAGE" --value "${STEP_NAME} update PR creation failed! :gopher_lift: :rotating_light:\n\nCheck build here: $BITRISE_BUILD_URL"
      envman add --key "SLACK_EMOJI" --value ":gradle:"
      envman add --key "SLACK_COLOR" --value "#ee003b"
      echo "Failed to create pull request. Response: $pr_response"
      exit 0
    fi
else
  echo "No changes detected, skipping commit."
  exit 0
fi
