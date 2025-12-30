package manager

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ops-system/pkg/protocol"
)

type ExportManager struct {
	sysMgr  *SystemManager
	pkgMgr  *PackageManager
	instMgr *InstanceManager // [新增]

	runnerPath string
}

// 修改构造函数，接收 InstanceManager
func NewExportManager(sysMgr *SystemManager, pkgMgr *PackageManager, instMgr *InstanceManager, uploadDir string) *ExportManager {
	return &ExportManager{
		sysMgr:     sysMgr,
		pkgMgr:     pkgMgr,
		instMgr:    instMgr, // [新增]
		runnerPath: filepath.Join(uploadDir, "tools"),
	}
}

// ExportSystem 导出系统为 ZIP 流
func (em *ExportManager) ExportSystem(systemID string, w io.Writer, targetOS string) error {
	// 1. 获取系统和模块信息
	// 【关键修复】传入真实的 instMgr，而不是 nil
	sysViewInterface := em.sysMgr.GetFullView(em.instMgr)

	views := sysViewInterface.([]protocol.SystemView)

	var targetSys *protocol.SystemView
	for _, v := range views {
		if v.ID == systemID {
			targetSys = &v
			break
		}
	}

	if targetSys == nil {
		return fmt.Errorf("system not found")
	}

	zw := zip.NewWriter(w)
	defer zw.Close()

	runnerManifest := protocol.RunnerManifest{
		SystemName: targetSys.Name,
		ExportTime: time.Now().Unix(),
		Modules:    []protocol.RunnerModule{},
	}

	for _, mod := range targetSys.Modules {
		log.Printf("Exporting module: %s (%s v%s)", mod.ModuleName, mod.PackageName, mod.PackageVersion)

		srcStream, err := em.pkgMgr.GetPackageStream(mod.PackageName, mod.PackageVersion)
		if err != nil {
			return fmt.Errorf("get package %s failed: %v", mod.PackageName, err)
		}

		tmpZip, _ := os.CreateTemp("", "export-*.zip")
		io.Copy(tmpZip, srcStream)
		srcStream.Close()

		zipR, err := zip.OpenReader(tmpZip.Name())
		if err != nil {
			tmpZip.Close()
			os.Remove(tmpZip.Name())
			return fmt.Errorf("read package zip failed: %v", err)
		}

		modDirName := strings.ReplaceAll(mod.ModuleName, " ", "_")
		basePath := fmt.Sprintf("services/%s/", modDirName)
		var svcManifest protocol.ServiceManifest

		for _, f := range zipR.File {
			if f.Name == "service.json" {
				rc, _ := f.Open()
				json.NewDecoder(rc).Decode(&svcManifest)
				rc.Close()
				continue
			}
			destName := basePath + f.Name
			header, _ := zip.FileInfoHeader(f.FileInfo())
			header.Name = destName
			header.Method = zip.Deflate
			writer, _ := zw.CreateHeader(header)
			rc, _ := f.Open()
			io.Copy(writer, rc)
			rc.Close()
		}

		zipR.Close()
		tmpZip.Close()
		os.Remove(tmpZip.Name())

		rMod := protocol.RunnerModule{
			Name:           mod.ModuleName,
			WorkDir:        filepath.FromSlash("services/" + modDirName),
			Entrypoint:     svcManifest.Entrypoint,
			Args:           svcManifest.Args,
			Env:            svcManifest.Env,
			StartOrder:     mod.StartOrder,
			StopEntrypoint: svcManifest.StopEntrypoint,
			StopArgs:       svcManifest.StopArgs,
		}
		runnerManifest.Modules = append(runnerManifest.Modules, rMod)
	}

	mj, _ := json.MarshalIndent(runnerManifest, "", "  ")
	mw, _ := zw.Create("manifest.json")
	mw.Write(mj)

	runnerName := "runner"
	if targetOS == "windows" {
		runnerName += ".exe"
	}

	runnerSrc := filepath.Join(em.runnerPath, runnerName)
	if f, err := os.Open(runnerSrc); err == nil {
		info, _ := f.Stat()
		header, _ := zip.FileInfoHeader(info)
		header.Name = runnerName
		header.SetMode(0755)
		rw, _ := zw.CreateHeader(header)
		io.Copy(rw, f)
		f.Close()
	} else {
		readme, _ := zw.Create("README.txt")
		readme.Write([]byte("Please download 'runner' tool and place it here to start."))
	}

	return nil
}
