package cri

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContainerdConfigCgroupDriver(t *testing.T) {
	for _, tc := range []struct {
		name, config, want, wantErr string
	}{
		{"v2 systemd", `version = 2
[plugins."io.containerd.grpc.v1.cri".containerd]
default_runtime_name = "runc"
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc.options]
SystemdCgroup = true
`, DefaultSystemdCgroupDriver, ""},
		{"v3 systemd", `version = 3
[plugins."io.containerd.cri.v1.runtime".containerd]
default_runtime_name = "crun"
[plugins."io.containerd.cri.v1.runtime".containerd.runtimes.crun.options]
SystemdCgroup = true
`, DefaultSystemdCgroupDriver, ""},
		{"v4 systemd", `version = 4
[plugins."io.containerd.cri.v1.runtime".containerd]
default_runtime_name = "runc"
[plugins."io.containerd.cri.v1.runtime".containerd.runtimes.runc.options]
SystemdCgroup = true
`, DefaultSystemdCgroupDriver, ""},
		{"v3 cgroupfs", `version = 3
[plugins."io.containerd.cri.v1.runtime".containerd.runtimes.runc.options]
SystemdCgroup = false
`, DefaultCgroupDriver, ""},
		{"missing runtime", "version = 3\n", "", "not found"},
		{"imports refused", "version = 4\nimports = ['other.toml']\n", "", "imports are not supported"},
		{"unknown version", "version = 5\n", "", "unsupported"},
		{"bad toml", "version = [\n", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(tc.config), 0600); err != nil {
				t.Fatal(err)
			}
			rt := &ContainerdRuntime{config: path}
			got, err := rt.processConfigFile()
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if tc.name == "bad toml" {
				if err == nil {
					t.Fatal("expected TOML parse error")
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}
