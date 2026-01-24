package registry

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Embedded JSON schemas from Bazel Central Registry.
// These are the canonical schemas that BCR uses for validation.
var (
	//go:embed schema/metadata.schema.json
	metadataSchemaJSON string

	//go:embed schema/source.schema.json
	sourceSchemaJSON string
)

// Validator validates registry JSON data against BCR schemas.
type Validator struct {
	compiler *jsonschema.Compiler
	schemas  struct {
		metadata *jsonschema.Schema
		source   *jsonschema.Schema
	}
	once sync.Once
	err  error
}

// NewValidator creates a validator with BCR schemas.
func NewValidator() *Validator {
	return &Validator{
		compiler: jsonschema.NewCompiler(),
	}
}

// init lazily initializes the schemas on first use.
func (v *Validator) init() error {
	v.once.Do(func() {
		v.err = v.compileSchemas()
	})
	return v.err
}

func (v *Validator) compileSchemas() error {
	// Parse and add metadata schema
	metadataDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader([]byte(metadataSchemaJSON)))
	if err != nil {
		return fmt.Errorf("failed to parse metadata schema: %w", err)
	}
	if err := v.compiler.AddResource("metadata.schema.json", metadataDoc); err != nil {
		return fmt.Errorf("failed to add metadata schema: %w", err)
	}

	// Parse and add source schema
	sourceDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader([]byte(sourceSchemaJSON)))
	if err != nil {
		return fmt.Errorf("failed to parse source schema: %w", err)
	}
	if err := v.compiler.AddResource("source.schema.json", sourceDoc); err != nil {
		return fmt.Errorf("failed to add source schema: %w", err)
	}

	// Compile metadata schema
	schema, err := v.compiler.Compile("metadata.schema.json")
	if err != nil {
		return fmt.Errorf("failed to compile metadata schema: %w", err)
	}
	v.schemas.metadata = schema

	// Compile source schema
	schema, err = v.compiler.Compile("source.schema.json")
	if err != nil {
		return fmt.Errorf("failed to compile source schema: %w", err)
	}
	v.schemas.source = schema

	return nil
}

// ValidateMetadata validates JSON data against the metadata.json schema.
func (v *Validator) ValidateMetadata(data []byte) error {
	if err := v.init(); err != nil {
		return err
	}

	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	return v.schemas.metadata.Validate(doc)
}

// ValidateSource validates JSON data against the source.json schema.
func (v *Validator) ValidateSource(data []byte) error {
	if err := v.init(); err != nil {
		return err
	}

	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	return v.schemas.source.Validate(doc)
}

// ValidateMetadataStruct validates a Metadata struct.
func (v *Validator) ValidateMetadataStruct(m *Metadata) error {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}
	return v.ValidateMetadata(data)
}

// ValidateSourceStruct validates a Source struct.
func (v *Validator) ValidateSourceStruct(s *Source) error {
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("failed to marshal source: %w", err)
	}
	return v.ValidateSource(data)
}

// ValidationError wraps schema validation errors with context.
type ValidationError struct {
	Field   string
	Message string
	Cause   error
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s", e.Field, e.Message)
	}
	return e.Message
}

func (e *ValidationError) Unwrap() error {
	return e.Cause
}
