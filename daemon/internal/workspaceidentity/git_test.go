package workspaceidentity

import "testing"

func TestNormalizeRemoteRemovesCredentialsAndUnifiesSSHHTTPS(t *testing.T) {
	https := KeyFromRemote("https://user:secret@GitHub.com/Intellif/Project_Manager.git?token=hidden")
	ssh := KeyFromRemote("git@github.com:Intellif/Project_Manager.git")
	if https == "" || https != ssh {
		t.Fatalf("repository keys differ: https=%q ssh=%q", https, ssh)
	}
	if len(https) != 64 {
		t.Fatalf("repository key length = %d", len(https))
	}
}

func TestNormalizeRemoteKeepsDifferentRepositoriesSeparate(t *testing.T) {
	first := KeyFromRemote("git@github.com:intellif/project-a.git")
	second := KeyFromRemote("git@github.com:intellif/project-b.git")
	if first == second {
		t.Fatal("different repositories received the same key")
	}
}
