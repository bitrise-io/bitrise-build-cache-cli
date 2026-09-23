package stringmerge

import "strings"

// ChangeContentInBlock - checks the currentContent whether a `blockStartPattern` and `blockEndPattern` block is already present.
// If there is, then only the block's content will be modified.
// If there's no marked block in the content yet then append it to the existing content
// with the `blockContentStr` content in the block.
func ChangeContentInBlock(currentContent, blockStartPattern, blockEndPattern, blockContentStr string) string {
	fullBlockContent := blockStartPattern + "\n" + blockContentStr + "\n" + blockEndPattern + "\n"

	// if current content is empty then just return the block content
	if len(currentContent) < 1 {
		return fullBlockContent
	}

	// check if the block is already present
	startIndex := strings.Index(currentContent, blockStartPattern)
	endIndex := strings.Index(currentContent, blockEndPattern)

	if startIndex > -1 && endIndex > -1 && startIndex < endIndex {
		// the block is already present, only replace the content
		return currentContent[:startIndex] +
			blockStartPattern + "\n" +
			blockContentStr + "\n" +
			currentContent[endIndex:]
	}

	// the block is not present yet, append it to the existing content
	return currentContent + "\n" + fullBlockContent
}

// RemoveBlock strips a `blockStartPattern`..`blockEndPattern` block (inclusive of
// the end-marker line and its trailing newline) from currentContent. It also
// collapses the "\n\n" left when ChangeContentInBlock had inserted a blank-line
// separator while appending, so a round-trip Activate→Deactivate returns the
// original content. Returns the input unchanged when either marker is missing or
// when the markers are out of order.
func RemoveBlock(currentContent, blockStartPattern, blockEndPattern string) string {
	startIndex := strings.Index(currentContent, blockStartPattern)
	if startIndex < 0 {
		return currentContent
	}

	endIndex := strings.Index(currentContent, blockEndPattern)
	if endIndex < 0 || endIndex < startIndex {
		return currentContent
	}

	cutTo := endIndex + len(blockEndPattern)
	if cutTo < len(currentContent) && currentContent[cutTo] == '\n' {
		cutTo++
	}

	result := currentContent[:startIndex] + currentContent[cutTo:]

	// ChangeContentInBlock's append path prefixes a "\n" separator; after the
	// block+trailing-\n are gone that leaves "\n\n" at the seam (or "\n" at EOF).
	// Collapse both cases so activate→deactivate is a true round-trip.
	if startIndex > 0 && currentContent[startIndex-1] == '\n' {
		// EOF case: block ran to end of file, leaving one extra "\n" in the tail.
		if startIndex == len(result) && strings.HasSuffix(result, "\n\n") {
			result = result[:len(result)-1]
		}

		// Mid-file case: startIndex now points to the char after the seam. If
		// that char is also '\n' we have "\n\n" back-to-back — collapse.
		if startIndex < len(result) && result[startIndex] == '\n' {
			result = result[:startIndex] + result[startIndex+1:]
		}
	}

	return result
}
