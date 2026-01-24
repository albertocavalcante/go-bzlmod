package main

import (
	"context"
	"fmt"
	"log"

	gobzlmod "github.com/albertocavalcante/go-bzlmod"
)

func main() {
	fmt.Println("Go-bzlmod Example")
	fmt.Println("===================")

	// Resolve dependencies from the example MODULE.bazel file
	resolutionList, err := gobzlmod.ResolveFile(
		context.Background(),
		"../MODULE.bazel",
		gobzlmod.ResolutionOptions{
			Registries:     []string{"https://bcr.bazel.build"},
			IncludeDevDeps: true,
		},
	)
	if err != nil {
		log.Fatalf("❌ Failed to resolve dependencies: %v", err)
	}

	// Print summary
	fmt.Printf("\n📊 Summary:\n")
	fmt.Printf("   Total modules: %d\n", resolutionList.Summary.TotalModules)
	fmt.Printf("   Production: %d\n", resolutionList.Summary.ProductionModules)
	fmt.Printf("   Development: %d\n", resolutionList.Summary.DevModules)

	// Group modules by type
	var prodModules, devModules []gobzlmod.ModuleToResolve
	for _, module := range resolutionList.Modules {
		if module.DevDependency {
			devModules = append(devModules, module)
		} else {
			prodModules = append(prodModules, module)
		}
	}

	// Print production dependencies
	if len(prodModules) > 0 {
		fmt.Printf("\n🏭 Production Dependencies:\n")
		for _, module := range prodModules {
			fmt.Printf("   %s@%s\n", module.Name, module.Version)
		}
	}

	// Print development dependencies
	if len(devModules) > 0 {
		fmt.Printf("\n🔧 Development Dependencies:\n")
		for _, module := range devModules {
			fmt.Printf("   %s@%s\n", module.Name, module.Version)
		}
	}

	fmt.Println("\n✅ Dependency resolution completed!")
}
