// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package voteoptions

// Config stores the vote info configuration.
type Config struct {
	VoteInfo    string
	VoteVersion uint32
}

// VoteOptions is the main handler for retrieving vote options.
type VoteOptions struct {
	cfg *Config
}

// Config returns the current vote options config.
func (vo *VoteOptions) Config() (*Config, error) {
	config := &Config{
		VoteInfo:    vo.cfg.VoteInfo,
		VoteVersion: vo.cfg.VoteVersion,
	}
	return config, nil
}

// NewVoteOptions creates a new VoteOptions.
func NewVoteOptions(cfg *Config) (*VoteOptions, error) {
	return &VoteOptions{
		cfg: cfg,
	}, nil
}
