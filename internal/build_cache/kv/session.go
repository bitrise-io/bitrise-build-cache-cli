package kv

func (c *Client) ChangeSession(invocationID string, appSlug string, buildSlug string, stepSlug string) {
	c.invocationID = invocationID
	c.cacheConfigMetadata.BitriseAppID = appSlug
	c.cacheConfigMetadata.BitriseBuildID = buildSlug
	c.cacheConfigMetadata.BitriseStepExecutionID = stepSlug
}
