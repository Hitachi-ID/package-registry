// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package packages

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	yamlv2 "gopkg.in/yaml.v2"

	ucfg "github.com/elastic/go-ucfg"
	"github.com/elastic/go-ucfg/yaml"

	"github.com/elastic/package-registry/util"
)

const (
	DirIngestPipeline = "ingest_pipeline"

	DefaultPipelineName     = "default"
	DefaultPipelineNameJSON = "default.json"
	DefaultPipelineNameYAML = "default.yml"
)

var validTypes = map[string]string{
	"logs":       "Logs",
	"metrics":    "Metrics",
	"synthetics": "Synthetics",
	"traces":     "Traces",
}

type DataStream struct {
	// Name and type of the data stream. This is linked to data_stream.dataset and data_stream.type fields.
	Type            string `config:"type" json:"type" validate:"required"`
	Dataset         string `config:"dataset" json:"dataset,omitempty" yaml:"dataset,omitempty"`
	Hidden          bool   `config:"hidden" json:"hidden,omitempty" yaml:"hidden,omitempty"`
	IlmPolicy       string `config:"ilm_policy" json:"ilm_policy,omitempty" yaml:"ilm_policy,omitempty"`
	DatasetIsPrefix bool   `config:"dataset_is_prefix" json:"dataset_is_prefix,omitempty" yaml:"dataset_is_prefix,omitempty"`

	Title   string `config:"title" json:"title" validate:"required"`
	Release string `config:"release" json:"release"`

	// Deprecated: Replaced by elasticsearch.ingest_pipeline.name
	IngestPipeline string         `config:"ingest_pipeline,omitempty" config:"ingest_pipeline" json:"ingest_pipeline,omitempty" yaml:"ingest_pipeline,omitempty"`
	Streams        []Stream       `config:"streams" json:"streams,omitempty" yaml:"streams,omitempty" `
	Package        string         `json:"package,omitempty" yaml:"package,omitempty"`
	Elasticsearch  *Elasticsearch `config:"elasticsearch,omitempty" json:"elasticsearch,omitempty" yaml:"elasticsearch,omitempty"`

	// Generated fields
	Path string `json:"path,omitempty" yaml:"path,omitempty"`

	// Local path to the data stream directory, relative to the package directory
	BasePath string `json:"-" yaml:"-"`

	// Reference to the package containing this data stream
	packageRef *Package
}

type Input struct {
	Type         string     `config:"type" json:"type" validate:"required"`
	Vars         []Variable `config:"vars" json:"vars,omitempty" yaml:"vars,omitempty"`
	Title        string     `config:"title" json:"title,omitempty" yaml:"title,omitempty"`
	Description  string     `config:"description" json:"description,omitempty" yaml:"description,omitempty"`
	Streams      []Stream   `config:"streams" json:"streams,omitempty" yaml:"streams,omitempty"`
	TemplatePath string     `config:"template_path" json:"template_path,omitempty" yaml:"template_path,omitempty"`
	InputGroup   string     `config:"input_group" json:"input_group,omitempty" yaml:"input_group,omitempty"`
}

type Stream struct {
	Input      string     `config:"input" json:"input" validate:"required"`
	Vars       []Variable `config:"vars" json:"vars,omitempty" yaml:"vars,omitempty"`
	DataStream string     `config:"data_stream" json:"data_stream,omitempty" yaml:"data_stream,omitempty"`
	// TODO: This might cause issues when consuming the json as the key contains . (had been an issue in the past if I remember correctly)
	TemplatePath string `config:"template_path" json:"template_path,omitempty" yaml:"template_path,omitempty"`
	Title        string `config:"title" json:"title,omitempty" yaml:"title,omitempty"`
	Description  string `config:"description" json:"description,omitempty" yaml:"description,omitempty"`
	Enabled      *bool  `config:"enabled" json:"enabled,omitempty" yaml:"enabled,omitempty"`
}

type Variable struct {
	Name        string      `config:"name" json:"name" yaml:"name"`
	Type        string      `config:"type" json:"type" yaml:"type"`
	Title       string      `config:"title" json:"title,omitempty" yaml:"title,omitempty"`
	Description string      `config:"description" json:"description,omitempty" yaml:"description,omitempty"`
	Multi       bool        `config:"multi" json:"multi" yaml:"multi"`
	Required    bool        `config:"required" json:"required" yaml:"required"`
	ShowUser    bool        `config:"show_user" json:"show_user" yaml:"show_user"`
	Default     interface{} `config:"default" json:"default,omitempty" yaml:"default,omitempty"`
}

type Elasticsearch struct {
	IndexTemplateSettings map[string]interface{}   `config:"index_template.settings" json:"index_template.settings,omitempty" yaml:"index_template.settings,omitempty"`
	IndexTemplateMappings map[string]interface{}   `config:"index_template.mappings" json:"index_template.mappings,omitempty" yaml:"index_template.mappings,omitempty"`
	IngestPipelineName    string                   `config:"ingest_pipeline.name,omitempty" json:"ingest_pipeline.name,omitempty" yaml:"ingest_pipeline.name,omitempty"`
	Privileges            *ElasticsearchPrivileges `config:"privileges,omitempty" json:"privileges,omitempty" yaml:"privileges,omitempty"`
}

type ElasticsearchPrivileges struct {
	Indices []string `config:"indices,omitempty" json:"indices,omitempty" yaml:"indices,omitempty"`
}

type fieldEntry struct {
	name  string
	aType string
}

func NewDataStream(basePath string, p *Package) (*DataStream, error) {
	fs, err := p.fs()
	if err != nil {
		return nil, err
	}
	defer fs.Close()

	manifestPath := filepath.Join(basePath, "manifest.yml")

	// Check if manifest exists
	_, err = fs.Stat(manifestPath)
	if err != nil && os.IsNotExist(err) {
		return nil, errors.Wrapf(err, "manifest does not exist for data stream: %s", p.BasePath)
	}

	dataStreamPath := filepath.Base(basePath)

	b, err := ReadAll(fs, manifestPath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read manifest: %s", err)
	}

	manifest, err := yaml.NewConfig(b, ucfg.PathSep("."))
	if err != nil {
		return nil, errors.Wrapf(err, "error creating new manifest config")
	}
	var d = &DataStream{
		Package:    p.Name,
		packageRef: p,

		// This is the name of the directory of the dataStream
		Path:     dataStreamPath,
		BasePath: basePath,
	}

	// go-ucfg automatically calls the `Validate` method on the DataStream object here
	err = manifest.Unpack(d, ucfg.PathSep("."))
	if err != nil {
		return nil, errors.Wrapf(err, "error building data stream (path: %s) in package: %s", dataStreamPath, p.Name)
	}

	// if id is not set, {package}.{dataStreamPath} is the default
	if d.Dataset == "" {
		d.Dataset = p.Name + "." + dataStreamPath
	}

	if d.Release == "" {
		d.Release = DefaultRelease
	}

	// Default for the enabled flags is true.
	trueValue := true
	for i, _ := range d.Streams {
		if d.Streams[i].Enabled == nil {
			d.Streams[i].Enabled = &trueValue
		}

		// TODO: validate that the template path actually exists
		if d.Streams[i].TemplatePath == "" {
			d.Streams[i].TemplatePath = "stream.yml.hbs"
		}
	}

	if !IsValidRelease(d.Release) {
		return nil, fmt.Errorf("invalid release: %s", d.Release)
	}

	pipelineDir := filepath.Join(d.BasePath, "elasticsearch", DirIngestPipeline)
	paths, err := fs.Glob(filepath.Join(pipelineDir, "*"))
	if err != nil {
		return nil, err
	}

	if d.Elasticsearch != nil && d.Elasticsearch.IngestPipelineName == "" {
		// Check that no ingest pipeline exists in the directory except default
		for _, path := range paths {
			if filepath.Base(path) == DefaultPipelineNameJSON || filepath.Base(path) == DefaultPipelineNameYAML {
				d.Elasticsearch.IngestPipelineName = DefaultPipelineName
				// TODO: remove because of legacy
				d.IngestPipeline = DefaultPipelineName
				break
			}
		}
		// TODO: Remove, only here for legacy
	} else if d.IngestPipeline == "" {
		// Check that no ingest pipeline exists in the directory except default
		for _, path := range paths {
			if filepath.Base(path) == DefaultPipelineNameJSON || filepath.Base(path) == DefaultPipelineNameYAML {
				d.IngestPipeline = DefaultPipelineName
				break
			}
		}
	}
	if d.IngestPipeline == "" && len(paths) > 0 {
		return nil, fmt.Errorf("unused pipelines in the package (dataset: %s): %s", d.Dataset, strings.Join(paths, ","))
	}
	return d, nil
}

func (d *DataStream) Validate() error {
	if ValidationDisabled {
		return nil
	}

	if strings.Contains(d.Dataset, "-") {
		return fmt.Errorf("data stream name is not allowed to contain `-`: %s", d.Dataset)
	}

	if !d.validType() {
		return fmt.Errorf("type is not valid: %s", d.Type)
	}

	fs, err := d.packageRef.fs()
	if err != nil {
		return err
	}
	defer fs.Close()

	// In case an ingest pipeline is set, check if it is around
	pipelineDir := filepath.Join(d.BasePath, "elasticsearch", DirIngestPipeline)
	if d.IngestPipeline != "" {
		var validFound bool

		jsonPipelinePath := filepath.Join(pipelineDir, d.IngestPipeline+".json")
		_, errJSON := fs.Stat(jsonPipelinePath)
		if errJSON != nil && !os.IsNotExist(errJSON) {
			return errors.Wrapf(errJSON, "stat ingest pipeline JSON file failed (path: %s)", jsonPipelinePath)
		}
		if !os.IsNotExist(errJSON) {
			err := validateIngestPipelineFile(fs, jsonPipelinePath)
			if err != nil {
				return errors.Wrapf(err, "validating ingest pipeline JSON file failed (path: %s)", jsonPipelinePath)
			}
			validFound = true
		}

		yamlPipelinePath := filepath.Join(pipelineDir, d.IngestPipeline+".yml")
		_, errYAML := fs.Stat(yamlPipelinePath)
		if errYAML != nil && !os.IsNotExist(errYAML) {
			return errors.Wrapf(errYAML, "stat ingest pipeline YAML file failed (path: %s)", jsonPipelinePath)
		}
		if !os.IsNotExist(errYAML) {
			err := validateIngestPipelineFile(fs, yamlPipelinePath)
			if err != nil {
				return errors.Wrapf(err, "validating ingest pipeline YAML file failed (path: %s)", jsonPipelinePath)
			}
			validFound = true
		}

		if !validFound {
			return fmt.Errorf("defined ingest_pipeline does not exist: %s", pipelineDir+d.IngestPipeline)
		}
	}

	err = d.validateRequiredFields(fs)
	if err != nil {
		return errors.Wrap(err, "validating required fields failed")
	}
	return nil
}

func (d *DataStream) validType() bool {
	_, exists := validTypes[d.Type]
	return exists
}

func validateIngestPipelineFile(fs PackageFileSystem, pipelinePath string) error {
	f, err := ReadAll(fs, pipelinePath)
	if err != nil {
		return errors.Wrapf(err, "reading ingest pipeline file failed (path: %s)", pipelinePath)
	}

	ext := filepath.Ext(pipelinePath)
	var m map[string]interface{}
	switch ext {
	case ".json":
		err = json.Unmarshal(f, &m)
	case ".yml":
		err = yamlv2.Unmarshal(f, &m)
	default:
		return fmt.Errorf("unsupported pipeline extension (path: %s, ext: %s)", pipelinePath, ext)
	}
	return err
}

// validateRequiredFields method loads fields from all files and checks if required fields are present.
func (d *DataStream) validateRequiredFields(fs PackageFileSystem) error {
	fieldsDirPath := filepath.Join(d.BasePath, "fields")

	// Collect fields from all files
	fieldsFiles, err := fs.Glob(filepath.Join(fieldsDirPath, "*"))
	if err != nil {
		return err
	}
	var allFields []util.MapStr
	for _, path := range fieldsFiles {
		body, err := ReadAll(fs, path)
		if err != nil {
			return errors.Wrapf(err, "reading file failed (path: %s)", path)
		}

		var m []util.MapStr
		err = yamlv2.Unmarshal(body, &m)
		if err != nil {
			return errors.Wrapf(err, "unmarshaling file failed (path: %s)", path)
		}

		allFields = append(allFields, m...)
		return nil
	}
	if err != nil {
		return errors.Wrapf(err, "walking through fields files failed")
	}

	// Flatten all fields
	for i, fields := range allFields {
		allFields[i] = fields.Flatten()
	}

	// Verify required keys
	err = requireField(allFields, "data_stream.type", "constant_keyword", err)
	err = requireField(allFields, "data_stream.dataset", "constant_keyword", err)
	err = requireField(allFields, "data_stream.namespace", "constant_keyword", err)
	err = requireField(allFields, "@timestamp", "date", err)
	return err
}

func requireField(allFields []util.MapStr, searchedName, expectedType string, validationErr error) error {
	if validationErr != nil {
		return validationErr
	}

	f, err := findField(allFields, searchedName)
	if err != nil {
		f, err = findFieldSplit(allFields, searchedName)
		if err != nil {
			return errors.Wrapf(err, "finding field failed (searchedName: %s)", searchedName)
		}
	}

	if f.aType != expectedType {
		return fmt.Errorf("wrong field type for '%s' (expected: %s, got: %s)", searchedName, expectedType, f.aType)
	}
	return nil
}

func findFieldSplit(allFields []util.MapStr, searchedName string) (*fieldEntry, error) {
	levels := strings.Split(searchedName, ".")
	curFields := allFields
	var err error
	for _, part := range levels[:len(levels)-1] {
		curFields, err = getFieldsArray(curFields, part)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to find fields array")
		}
	}
	return findField(curFields, levels[len(levels)-1])
}

func createMapStr(in interface{}) (util.MapStr, error) {
	m := make(util.MapStr)
	v, ok := in.(map[interface{}]interface{})
	if !ok {
		return nil, fmt.Errorf("unable to convert %v to known type", in)
	}
	for k, val := range v {
		m[fmt.Sprintf("%v", k)] = fmt.Sprintf("%v", val)
	}
	return m, nil
}

func getFieldsArray(allFields []util.MapStr, searchedName string) ([]util.MapStr, error) {
	for _, fields := range allFields {
		name, err := fields.GetValue("name")
		if err != nil {
			return nil, errors.Wrapf(err, "cannot get value (key: name)")
		}
		if name == searchedName {
			value, err := fields.GetValue("fields")
			if err != nil {
				return nil, errors.Wrapf(err, "cannot get fields")
			}

			if inArray, ok := value.([]interface{}); ok {
				m := make([]util.MapStr, 0, len(inArray))
				for _, in := range inArray {
					mapStr, err := createMapStr(in)
					if err != nil {
						return nil, errors.Wrapf(err, "cannot create MapStr")
					}
					m = append(m, mapStr)
				}
				return m, nil
			}
			return nil, fmt.Errorf("fields was not []MapStr")
		}
	}
	return nil, fmt.Errorf("field '%s' not found", searchedName)
}

func findField(allFields []util.MapStr, searchedName string) (*fieldEntry, error) {
	for _, fields := range allFields {
		name, err := fields.GetValue("name")
		if err != nil {
			return nil, errors.Wrapf(err, "cannot get value (key: name)")
		}

		if name != searchedName {
			continue
		}

		aType, err := fields.GetValue("type")
		if err != nil {
			return nil, errors.Wrapf(err, "cannot get value (key: type)")
		}

		if aType == "" {
			return nil, fmt.Errorf("field '%s' found, but type is undefined", searchedName)
		}

		return &fieldEntry{
			name:  name.(string),
			aType: aType.(string),
		}, nil
	}
	return nil, fmt.Errorf("field '%s' not found", searchedName)
}
