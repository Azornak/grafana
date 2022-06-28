package migration

import (
	"context"
	"encoding/json"
	"time"

	"github.com/grafana/grafana/pkg/bus"
	"github.com/grafana/grafana/pkg/events"
	"github.com/grafana/grafana/pkg/infra/log"
	"github.com/grafana/grafana/pkg/services/datasources"
	"github.com/grafana/grafana/pkg/services/featuremgmt"
	"github.com/grafana/grafana/pkg/services/secrets/kvstore"
	"github.com/grafana/grafana/pkg/services/sqlstore"
	"github.com/grafana/grafana/pkg/setting"
)

const (
	dataSourceSecretType = "datasource"
)

type DataSourceSecretMigrationService struct {
	sqlStore           *sqlstore.SQLStore
	dataSourcesService datasources.DataSourceService
	secretsStore       kvstore.SecretsKVStore
	features           featuremgmt.FeatureToggles
	log                log.Logger
	bus                bus.Bus
}

func ProvideDataSourceMigrationService(
	cfg *setting.Cfg, dataSourcesService datasources.DataSourceService,
	secretsStore kvstore.SecretsKVStore, features featuremgmt.FeatureToggles,
	sqlStore *sqlstore.SQLStore, bus bus.Bus,
) kvstore.SecretMigrationService {
	return &DataSourceSecretMigrationService{
		sqlStore:           sqlStore,
		dataSourcesService: dataSourcesService,
		secretsStore:       secretsStore,
		features:           features,
		log:                log.New("secret.migration"),
		bus:                bus,
	}
}

func (s *DataSourceSecretMigrationService) WaitForProvisioning() error {
	wait := false
	s.bus.AddEventListener(func(ctx context.Context, e *events.DataSourceCreated) error {
		wait = true
		return nil
	})
	time.After(5 * time.Second)
	if wait {
		return s.WaitForProvisioning()
	} else {
		return nil
	}
}

func (s *DataSourceSecretMigrationService) Run(ctx context.Context) error {
	s.WaitForProvisioning()
	return s.sqlStore.InTransaction(ctx, func(ctx context.Context) error {
		query := &datasources.GetDataSourcesQuery{}
		err := s.dataSourcesService.GetDataSources(ctx, query)
		if err != nil {
			return err
		}

		s.log.Debug("starting data source secret migration")
		for _, ds := range query.Result {
			hasMigration, _ := ds.JsonData.Get("secretMigrationComplete").Bool()
			if !hasMigration {
				secureJsonData, err := s.dataSourcesService.DecryptLegacySecrets(ctx, ds)
				if err != nil {
					return err
				}

				jsonData, err := json.Marshal(secureJsonData)
				if err != nil {
					return err
				}

				err = s.secretsStore.Set(ctx, ds.OrgId, ds.Name, dataSourceSecretType, string(jsonData))
				if err != nil {
					return err
				}

				ds.JsonData.Set("secretMigrationComplete", true)
				err = s.dataSourcesService.UpdateDataSource(ctx, &datasources.UpdateDataSourceCommand{Id: ds.Id, OrgId: ds.OrgId, Uid: ds.Uid, JsonData: ds.JsonData})
				if err != nil {
					return err
				}
			}

			if s.features.IsEnabled(featuremgmt.FlagDisableSecretsCompatibility) && len(ds.SecureJsonData) > 0 {
				err := s.dataSourcesService.DeleteDataSourceSecrets(ctx, &datasources.DeleteDataSourceSecretsCommand{UID: ds.Uid, OrgID: ds.OrgId, ID: ds.Id})
				if err != nil {
					return err
				}
			}

		}
		s.log.Debug("data source secret migration complete")
		return nil
	})
}
