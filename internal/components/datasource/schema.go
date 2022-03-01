package datasource

import (
	"embed"
	"io"
	"io/fs"
	"path/filepath"
	"reflect"
	"testing/fstest"

	"cuelang.org/go/cue"
	"github.com/grafana/grafana/internal/cuectx"
	"github.com/grafana/grafana/pkg/schema"
	"github.com/grafana/thema"
	"github.com/grafana/thema/kernel"
	"github.com/grafana/thema/load"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

func init() {
	lib := cuectx.ProvideThemaLibrary()
	lin, err := DatasourceLineage(lib)
	if err != nil {
		panic(err)
	}

	// Calling this ensures our program cannot start if the Go DataSource.Model type
	// is not aligned with the canonical schema version in our lineage
	_ = newDataSourceJSONKernel(lin)

	zsch, _ := lin.Schema(thema.SV(0, 0))
	if err = thema.AssignableTo(zsch, Model{}); err != nil {
		panic(err)
	}

	sch := schema.NewThemaSchema(lin, &Datasource{}, &CRList{})
	schema.RegisterCoreSchema(&sch)
}

//go:embed datasource.cue
var cueFS embed.FS

func DatasourceLineage(lib thema.Library, opts ...thema.BindOption) (thema.Lineage, error) {
	linval, err := loadDatasourceLineage(lib)
	if err != nil {
		return nil, err
	}
	return thema.BindLineage(linval, lib, opts...)
}

func loadDatasourceLineage(lib thema.Library) (cue.Value, error) {
	prefix := filepath.FromSlash("internal/components/datasource")
	fs, err := prefixWithGrafanaCUE(prefix, cueFS)
	if err != nil {
		return cue.Value{}, err
	}
	inst, err := load.InstancesWithThema(fs, prefix)

	// Need to trick loading by creating the embedded file and
	// making it look like a module in the root dir.
	if err != nil {
		return cue.Value{}, err
	}

	return lib.Context().BuildInstance(inst), nil
}

var _ thema.LineageFactory = DatasourceLineage

func newDataSourceJSONKernel(lin thema.Lineage) kernel.InputKernel {
	jdk, err := kernel.NewInputKernel(kernel.InputKernelConfig{
		Lineage:     lin,
		Loader:      kernel.NewJSONDecoder("datasource.cue"),
		To:          thema.SV(0, 0),
		TypeFactory: func() interface{} { return &Model{} },
	})
	if err != nil {
		panic(err)
	}
	return jdk
}

type Model struct {
	// Omitting these two at least for now, because sequential IDs == :(
	// Id                int64                  `json:"id"`
	// OrgId int64 `json:"orgId"` // May change, but to make store work

	//UID               string `json:"uid"` // May change, but to make store work
	//Name              string `json:"name"`
	Type              string `json:"type"`
	TypeLogoUrl       string `json:"typeLogoUrl"`
	Access            string `json:"access"` // enum: *"proxy" | "direct"
	Url               string `json:"url"`
	Password          string `json:"password"`
	User              string `json:"user"`
	Database          string `json:"database"`
	BasicAuth         bool   `json:"basicAuth"`
	BasicAuthUser     string `json:"basicAuthUser"`
	BasicAuthPassword string `json:"basicAuthPassword"`
	WithCredentials   bool   `json:"withCredentials,omitempty"`
	IsDefault         bool   `json:"isDefault"`
	// JsonData          *simplejson.Json       `json:"jsonData,omitempty"`
	JsonData         map[string]interface{} `json:"jsonData,omitempty"`
	//SecureJsonFields map[string]bool        `json:"secureJsonFields,omitempty"`
	Version          int                    `json:"version"`
	ReadOnly         bool                   `json:"readOnly"`
	// AccessControl     accesscontrol.Metadata `json:"accessControl,omitempty"`
	//AccessControl map[string]bool `json:"accessControl,omitempty"`
}

// TODO this should be generated by thema
type ModelStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
}

// TODO this should be generated by thema
type Datasource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   Model       `json:"spec,omitempty"`
	Status ModelStatus `json:"status,omitempty"`
}

func (in *Datasource) DeepCopyObject() runtime.Object {
	val := reflect.ValueOf(in).Elem()

	cpy := reflect.New(val.Type())
	cpy.Elem().Set(val)

	// This might panic, so consider adding `recover` and stuff.
	return cpy.Interface().(runtime.Object)
}

func (in Datasource) GetIdentifier() types.UID {
	return in.UID
}

// TODO this should be generated by thema
type CRList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Datasource `json:"items"`
}

func (in *CRList) DeepCopyObject() runtime.Object {
	panic("not implemented")
}

func prefixWithGrafanaCUE(prefix string, inputfs fs.FS) (fs.FS, error) {
	m := fstest.MapFS{
		filepath.Join("cue.mod", "module.cue"): &fstest.MapFile{Data: []byte(`module: "github.com/grafana/grafana"`)},
	}

	prefix = filepath.FromSlash(prefix)
	err := fs.WalkDir(inputfs, ".", (func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		f, err := inputfs.Open(path)
		if err != nil {
			return err
		}

		b, err := io.ReadAll(f)
		if err != nil {
			return err
		}

		m[filepath.Join(prefix, path)] = &fstest.MapFile{Data: []byte(b)}
		return nil
	}))

	return m, err
}
