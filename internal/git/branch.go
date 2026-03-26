package git

import "strings"

// ListLocalBranches returns all local branch names for the given repo.
func ListLocalBranches(repoPath string) ([]string, error) {
	out, err := Cmd(repoPath, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}
