package main

import (
	"flag"
	"fmt"
	"os"

	"ops-system/pkg/packer"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "init":
		handleInit()
	case "build":
		handleBuild()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func handleInit() {
	// pack-tool init [dir]
	targetDir := "."
	if len(os.Args) > 2 {
		targetDir = os.Args[2]
	}

	if err := packer.GenerateTemplate(targetDir); err != nil {
		fmt.Printf("❌ Init failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ Template 'service.json' generated in: %s\n", targetDir)
}

func handleBuild() {
	// pack-tool build <src> -o <out>
	buildCmd := flag.NewFlagSet("build", flag.ExitOnError)
	output := buildCmd.String("o", "", "Output zip file path (default: <name>_<version>.zip)")

	buildCmd.Parse(os.Args[2:])

	args := buildCmd.Args()
	if len(args) < 1 {
		fmt.Println("Usage: pack-tool build <source_dir> [-o output.zip]")
		os.Exit(1)
	}

	sourceDir := args[0]

	// 如果未指定输出路径，默认生成在当前目录下
	finalOutput := *output
	if finalOutput == "" {
		// 这里为了简单，暂定默认名，实际可以在 Pack 内部解析 json 后决定默认名
		// 但为了解耦，这里简单设为 package.zip，建议用户指定 -o
		finalOutput = "package.zip"
		fmt.Println("⚠️  No output path specified, using 'package.zip'")
	}

	fmt.Printf("📦 Packing '%s' -> '%s'...\n", sourceDir, finalOutput)

	if err := packer.Pack(sourceDir, finalOutput); err != nil {
		fmt.Printf("❌ Build failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ Build successful!")
}

func printUsage() {
	fmt.Println("Ops-System Package Tool")
	fmt.Println("Usage:")
	fmt.Println("  pack-tool init <directory>        Generate service.json template")
	fmt.Println("  pack-tool build <directory> -o <out.zip>  Validate and pack directory")
}
