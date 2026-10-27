package architecture_test

import (
	"bufio"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// Rule identifiers name the owning contract in each reported violation.
// The contracts themselves live in docs/agent-context/backend-contracts.md.
const (
	ruleTestPlacement     = "test-placement"
	ruleGeneratedContract = "generated-contract"
	ruleClassification    = "layer-classification"
	ruleBoundedContext    = "bounded-context"
	ruleCompositionRoot   = "composition-root"
	ruleModel             = "model-dependencies"
	ruleService           = "service-dependencies"
	ruleDTO               = "dto-dependencies"
	ruleTransport         = "transport-dependencies"
	ruleAdapter           = "adapter-dependencies"
	ruleSharedLibrary     = "shared-library-dependencies"
)

var ruleReasons = map[string]string{
	ruleTestPlacement:     "backend tests live under backend/tests and production code never imports them",
	ruleGeneratedContract: "api packages hold only protoc output; change the .proto definition and run make generate-proto",
	ruleClassification:    "every production package belongs to a documented layer",
	ruleBoundedContext:    "a bounded context imports another context's code only through shared pkg, internalauth or api contracts",
	ruleCompositionRoot:   "only cmd entrypoints import a context's app composition root",
	ruleModel:             "models import only models and shared libraries, never Gin, gRPC, protobuf, SQL or Docker SDK packages",
	ruleService:           "services import only models, services and shared libraries, never transports, DTOs, adapters, Gin, gRPC, protobuf, SQL or Docker SDK packages",
	ruleDTO:               "DTOs import only DTOs, models and shared libraries",
	ruleTransport:         "transports call services and never reach adapters directly",
	ruleAdapter:           "adapters implement models and consumer-owned service contracts and never depend on transports or DTOs",
	ruleSharedLibrary:     "shared libraries are not tied to a bounded context",
}

type layer int

const (
	layerUnclassified layer = iota
	layerEntrypoint
	layerComposition
	layerTransport
	layerSharedTransport
	layerAdapter
	layerDTO
	layerService
	layerModel
	layerContract
	layerShared
	layerTest
)

// internalLayers maps the segment after backend/internal/<context>/ to its layer.
var internalLayers = map[string]layer{
	"app":            layerComposition,
	"http":           layerTransport,
	"grpc":           layerTransport,
	"dto":            layerDTO,
	"service":        layerService,
	"model":          layerModel,
	"config":         layerModel,
	"repository":     layerAdapter,
	"infrastructure": layerAdapter,
}

// contextLayers classifies segments that exist in only one bounded context.
var contextLayers = map[string]layer{
	"core/dependencywait": layerService,
	"gateway/authscope":   layerService,
	"sso/lib":             layerAdapter,
}

type layerPolicy struct {
	rule                  string
	allowed               []layer
	forbidsInfrastructure bool
}

var productionLayers = []layer{
	layerEntrypoint, layerComposition, layerTransport, layerSharedTransport, layerAdapter,
	layerDTO, layerService, layerModel, layerContract, layerShared,
}

var policies = map[layer]layerPolicy{
	layerEntrypoint:  {allowed: productionLayers},
	layerComposition: {allowed: productionLayers},
	layerTransport: {rule: ruleTransport, allowed: []layer{
		layerTransport, layerSharedTransport, layerService, layerDTO, layerModel, layerContract, layerShared,
	}},
	layerSharedTransport: {rule: ruleSharedLibrary, allowed: []layer{layerShared}},
	layerAdapter: {rule: ruleAdapter, allowed: []layer{
		layerAdapter, layerSharedTransport, layerService, layerModel, layerContract, layerShared,
	}},
	layerDTO:     {rule: ruleDTO, allowed: []layer{layerDTO, layerModel, layerShared}},
	layerService: {rule: ruleService, allowed: []layer{layerService, layerModel, layerShared}, forbidsInfrastructure: true},
	layerModel:   {rule: ruleModel, allowed: []layer{layerModel, layerShared}, forbidsInfrastructure: true},
	layerShared:  {rule: ruleSharedLibrary, allowed: []layer{layerShared}},
}

// infrastructureCarriers are shared libraries that exist to map Gin, gRPC or SQL,
// so models and services must not import them either. pkg/logging is the accepted
// logging contract for every layer and stays allowed.
var infrastructureCarriers = map[string]bool{
	"pkg/httpresponse": true,
	"pkg/grpcerrors":   true,
	"pkg/pgpool":       true,
}

// infrastructureTechnology lists the import roots that models and services must not use.
var infrastructureTechnology = []string{
	"database/sql",
	"github.com/ClickHouse",
	"github.com/lib/pq",
	"github.com/docker",
	"github.com/moby",
	"github.com/gin-gonic",
	"github.com/gin-contrib",
	"google.golang.org/grpc",
	"google.golang.org/protobuf",
}

type pkgClass struct {
	context string
	layer   layer
}

type violation struct {
	rule     string
	file     string
	imported string
}

func (v violation) String() string {
	subject := v.file
	if v.imported != "" {
		subject += " imports " + v.imported
	}
	return fmt.Sprintf("%s: %s: %s", v.rule, subject, ruleReasons[v.rule])
}

type report struct {
	checkedFiles int
	violations   []violation
}

// checkTree applies the documented layer rules to every Go file of the module rooted at root.
func checkTree(root string) (report, error) {
	module, err := modulePath(root)
	if err != nil {
		return report{}, err
	}

	var rep report
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return skipDir(root, file, entry.Name())
		}
		if !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return fmt.Errorf("relative path of %s: %w", file, err)
		}
		rep.checkedFiles++
		found, err := checkFile(fset, module, file, filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		rep.violations = append(rep.violations, found...)
		return nil
	})
	if err != nil {
		return report{}, fmt.Errorf("walk %s: %w", root, err)
	}
	return rep, nil
}

// skipDir mirrors the directories the go command ignores, including nested modules.
func skipDir(root, dir, name string) error {
	if dir == root {
		return nil
	}
	if name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return filepath.SkipDir
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		return filepath.SkipDir
	}
	return nil
}

func checkFile(fset *token.FileSet, module, file, rel string) ([]violation, error) {
	dir := path.Dir(rel)
	if isUnder(dir, "tests") {
		return nil, nil
	}
	if strings.HasSuffix(rel, "_test.go") {
		return []violation{{rule: ruleTestPlacement, file: rel}}, nil
	}

	parsed, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", rel, err)
	}
	source := classify(dir)
	switch source.layer {
	case layerContract:
		// Generated contracts are assessed through their .proto definitions by make check-proto.
		if ast.IsGenerated(parsed) {
			return nil, nil
		}
		return []violation{{rule: ruleGeneratedContract, file: rel}}, nil
	case layerUnclassified:
		return []violation{{rule: ruleClassification, file: rel}}, nil
	}

	var found []violation
	for _, spec := range parsed.Imports {
		imported, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("import path in %s: %w", rel, err)
		}
		if rule := importRule(module, source, imported); rule != "" {
			found = append(found, violation{rule: rule, file: rel, imported: imported})
		}
	}
	return found, nil
}

// importRule returns the rule that forbids source from importing imported, or "" when it is allowed.
func importRule(module string, source pkgClass, imported string) string {
	policy := policies[source.layer]
	if imported != module && !strings.HasPrefix(imported, module+"/") {
		if policy.forbidsInfrastructure && isInfrastructureTechnology(imported) {
			return policy.rule
		}
		return ""
	}

	rel := strings.TrimPrefix(strings.TrimPrefix(imported, module), "/")
	target := classify(rel)
	switch {
	case policy.forbidsInfrastructure && infrastructureCarriers[rel]:
		return policy.rule
	case target.layer == layerTest:
		return ruleTestPlacement
	case target.layer == layerUnclassified:
		// Reported once where the unclassified package is declared.
		return ""
	case source.context != "" && target.context != "" && source.context != target.context:
		return ruleBoundedContext
	case target.layer == layerComposition && source.layer != layerEntrypoint && source.layer != layerComposition:
		return ruleCompositionRoot
	}
	for _, allowed := range policy.allowed {
		if allowed == target.layer {
			return ""
		}
	}
	return policy.rule
}

// classify maps a module-relative package directory to its bounded context and layer.
func classify(dir string) pkgClass {
	segments := strings.Split(dir, "/")
	switch segments[0] {
	case "cmd":
		if len(segments) == 2 {
			return pkgClass{context: segments[1], layer: layerEntrypoint}
		}
	case "api":
		if len(segments) >= 2 {
			return pkgClass{layer: layerContract}
		}
	case "pkg":
		if len(segments) >= 2 {
			return pkgClass{layer: layerShared}
		}
	case "tests":
		return pkgClass{layer: layerTest}
	case "internal":
		if dir == "internal/internalauth" {
			return pkgClass{layer: layerSharedTransport}
		}
		if len(segments) < 3 {
			break
		}
		if segments[2] == "grpc" && len(segments) >= 4 && segments[3] == "client" {
			// Outbound gRPC clients are adapters behind service-owned interfaces.
			return pkgClass{context: segments[1], layer: layerAdapter}
		}
		if l, ok := internalLayers[segments[2]]; ok {
			return pkgClass{context: segments[1], layer: l}
		}
		if l, ok := contextLayers[segments[1]+"/"+segments[2]]; ok {
			return pkgClass{context: segments[1], layer: l}
		}
	}
	return pkgClass{layer: layerUnclassified}
}

func isInfrastructureTechnology(imported string) bool {
	for _, root := range infrastructureTechnology {
		if imported == root || strings.HasPrefix(imported, root+"/") {
			return true
		}
	}
	return false
}

func isUnder(dir, top string) bool {
	return dir == top || strings.HasPrefix(dir, top+"/")
}

func modulePath(root string) (string, error) {
	file, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("open go.mod: %w", err)
	}
	defer func() { _ = file.Close() }() // read-only close

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if fields := strings.Fields(scanner.Text()); len(fields) >= 2 && fields[0] == "module" {
			return strings.Trim(fields[1], `"`), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	return "", errors.New("go.mod declares no module path")
}
