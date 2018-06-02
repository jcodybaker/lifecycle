package lifecycle

import (
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
)

type Builder struct {
	PlatformDir string
	Buildpacks  BuildpackGroup
	Out, Err    io.Writer
}

type Env interface {
	AppendDirs(baseDir string) error
	AddEnvDir(envDir string) error
	SetEnvDir(envDir string) error
	List() []string
}

type Process struct {
	Type    string `toml:"type"`
	Command string `toml:"command"`
}

type LaunchTOML struct {
	Processes []Process `toml:"processes"`
}

type BuildMetadata LaunchTOML

func (b *Builder) Build(appDir, launchDir, cacheDir string, env Env) (*BuildMetadata, error) {
	procMap := processMap{}
	for _, bp := range b.Buildpacks {
		bpLaunchDir := filepath.Join(launchDir, bp.ID)
		bpCacheDir := filepath.Join(cacheDir, bp.ID)
		if err := os.MkdirAll(bpLaunchDir, 0777); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(bpCacheDir, 0777); err != nil {
			return nil, err
		}
		buildPath, err := filepath.Abs(filepath.Join(bp.Dir, "bin", "build"))
		if err != nil {
			return nil, err
		}
		cmd := exec.Command(buildPath, bpLaunchDir, bpCacheDir, b.PlatformDir)
		cmd.Env = env.List()
		cmd.Dir = appDir
		cmd.Stdout = b.Out
		cmd.Stderr = b.Err
		if err := cmd.Run(); err != nil {
			return nil, err
		}
		if err := setupEnv(env, bpCacheDir); err != nil {
			return nil, err
		}
		var launch LaunchTOML
		if _, err := toml.DecodeFile(filepath.Join(bpLaunchDir, "launch.toml"), &launch); err != nil {
			return nil, err
		}
		procMap.add(launch.Processes)
	}
	return &BuildMetadata{
		Processes: procMap.list(),
	}, nil
}

type DevelopTOML struct {
	Processes []Process `toml:"processes"`
}

type DevelopMetadata DevelopTOML

func (b *Builder) Develop(appDir, cacheDir string, env Env) (*DevelopMetadata, error) {
	procMap := processMap{}
	for _, bp := range b.Buildpacks {
		bpCacheDir := filepath.Join(cacheDir, bp.ID)
		if err := os.MkdirAll(bpCacheDir, 0777); err != nil {
			return nil, err
		}
		cmd := exec.Command(
			filepath.Join(bp.Dir, "bin", "develop"),
			bpCacheDir, b.PlatformDir,
		)
		cmd.Env = env.List()
		cmd.Dir = appDir
		cmd.Stdout = b.Out
		cmd.Stderr = b.Err
		if err := cmd.Run(); err != nil {
			return nil, err
		}
		if err := setupEnv(env, bpCacheDir); err != nil {
			return nil, err
		}
		var develop DevelopTOML
		if _, err := toml.DecodeFile(filepath.Join(bpCacheDir, "develop.toml"), &develop); err != nil {
			return nil, err
		}
		procMap.add(develop.Processes)
	}
	return &DevelopMetadata{
		Processes: procMap.list(),
	}, nil
}

func setupEnv(env Env, cacheDir string) error {
	cacheFiles, err := ioutil.ReadDir(cacheDir)
	if err != nil {
		return err
	}
	if err := eachDir(cacheFiles, func(layer os.FileInfo) error {
		return env.AppendDirs(filepath.Join(cacheDir, layer.Name()))
	}); err != nil {
		return err
	}
	if err := eachDir(cacheFiles, func(layer os.FileInfo) error {
		return env.SetEnvDir(filepath.Join(cacheDir, layer.Name(), "env", "set"))
	}); err != nil {
		return err
	}
	return eachDir(cacheFiles, func(layer os.FileInfo) error {
		return env.AddEnvDir(filepath.Join(cacheDir, layer.Name(), "env", "add"))
	})
}

func eachDir(files []os.FileInfo, fn func(os.FileInfo) error) error {
	for _, f := range files {
		if !f.IsDir() {
			continue
		}
		if err := fn(f); err != nil {
			return err
		}
	}
	return nil
}

type processMap map[string]Process

func (m processMap) add(l []Process) {
	for _, proc := range l {
		m[proc.Type] = proc
	}
}

func (m processMap) list() []Process {
	var keys []string
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var procs []Process
	for _, key := range keys {
		procs = append(procs, m[key])
	}
	return procs
}
