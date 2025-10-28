package dns

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/devhat/ipfailover/internal/config"
	"github.com/devhat/ipfailover/pkg/errors"
	"github.com/devhat/ipfailover/pkg/interfaces"
	"go.uber.org/zap"
)

// Route53Provider implements DNSProvider for AWS Route53
type Route53Provider struct {
	config *config.Route53Config
	client *route53.Client
	logger *zap.Logger
}

// NewRoute53Provider creates a new Route53 DNS provider
func NewRoute53Provider(cfg *config.Route53Config, logger *zap.Logger) (*Route53Provider, error) {
	// Create AWS config
	awsConfig, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID,
			cfg.SecretAccessKey,
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := route53.NewFromConfig(awsConfig)

	return &Route53Provider{
		config: cfg,
		client: client,
		logger: logger,
	}, nil
}

// Name returns the provider name
func (r *Route53Provider) Name() string {
	return "route53"
}

// UpdateRecord updates or creates a DNS record
func (r *Route53Provider) UpdateRecord(ctx context.Context, record interfaces.DNSRecord) error {
	r.logger.Info("updating DNS record",
		zap.String("provider", "route53"),
		zap.String("record", record.Name),
		zap.String("type", record.Type),
		zap.String("value", record.Value),
	)

	// First, try to find existing record
	existingRecord, err := r.findRecord(ctx, record.Name, record.Type)
	if err != nil {
		return errors.NewDNSProviderError("route53", record.Name, err)
	}

	if existingRecord != nil {
		// Update existing record
		if err := r.updateExistingRecord(ctx, existingRecord, record); err != nil {
			return errors.NewDNSProviderError("route53", record.Name, err)
		}
		return nil
	}

	// Create new record
	if err := r.createNewRecord(ctx, record); err != nil {
		return errors.NewDNSProviderError("route53", record.Name, err)
	}
	return nil
}

// GetRecord retrieves an existing DNS record
func (r *Route53Provider) GetRecord(ctx context.Context, name string, rtype string) (*interfaces.DNSRecord, error) {
	r.logger.Debug("getting DNS record",
		zap.String("provider", "route53"),
		zap.String("record", name),
		zap.String("type", rtype),
	)

	// Validate record type is not empty
	if rtype == "" {
		return nil, errors.NewDNSProviderError("route53", name, fmt.Errorf("empty record type"))
	}

	records, err := r.listRecords(ctx)
	if err != nil {
		return nil, errors.NewDNSProviderError("route53", name, err)
	}

	for _, record := range records {
		// Skip records where record.Name is nil
		if record.Name == nil {
			continue
		}

		if *record.Name == name && string(record.Type) == rtype {
			var value string
			// Only access record.ResourceRecords[0] if len > 0 and record.ResourceRecords[0].Value != nil
			if len(record.ResourceRecords) > 0 && record.ResourceRecords[0].Value != nil {
				value = *record.ResourceRecords[0].Value
			}

			// Verify record.TTL != nil before converting to int and default to 0 if nil
			var ttl int
			if record.TTL != nil {
				ttl = int(*record.TTL)
			}

			// Ensure Metadata map uses only non-nil values with fallbacks
			metadata := make(map[string]string)
			if record.Name != nil {
				metadata["route53_id"] = *record.Name
			}

			return &interfaces.DNSRecord{
				Name:     *record.Name,
				Type:     string(record.Type),
				Value:    value,
				TTL:      ttl,
				Provider: "route53",
				Metadata: metadata,
			}, nil
		}
	}

	return nil, nil // Record not found
}

// DeleteRecord deletes a DNS record
func (r *Route53Provider) DeleteRecord(ctx context.Context, name, recordType string) error {
	r.logger.Info("deleting DNS record",
		zap.String("provider", "route53"),
		zap.String("record", name),
		zap.String("type", recordType),
	)

	// Validate record type is not empty
	if recordType == "" {
		return errors.NewDNSProviderError("route53", name, fmt.Errorf("empty record type"))
	}

	record, err := r.findRecord(ctx, name, recordType)
	if err != nil {
		return errors.NewDNSProviderError("route53", name, err)
	}

	if record == nil {
		r.logger.Warn("record not found for deletion",
			zap.String("provider", "route53"),
			zap.String("record", name),
			zap.String("type", recordType),
		)
		return nil // Record doesn't exist, consider it deleted
	}

	if err := r.deleteRecord(ctx, record); err != nil {
		return errors.NewDNSProviderError("route53", name, err)
	}

	return nil
}

// Validate checks if the provider configuration is valid
func (r *Route53Provider) Validate(ctx context.Context) error {
	r.logger.Debug("validating Route53 provider configuration")

	// Test API access by listing hosted zone
	_, err := r.client.GetHostedZone(ctx, &route53.GetHostedZoneInput{
		Id: aws.String(r.config.HostedZoneID),
	})
	if err != nil {
		return errors.NewDNSProviderError("route53", "validation", err)
	}

	r.logger.Info("Route53 provider validation successful")
	return nil
}

// findRecord finds a record by name and type
func (r *Route53Provider) findRecord(ctx context.Context, name, recordType string) (*types.ResourceRecordSet, error) {
	records, err := r.listRecords(ctx)
	if err != nil {
		return nil, err
	}

	for _, record := range records {
		if record.Name != nil && *record.Name == name && (recordType == "" || string(record.Type) == recordType) {
			rec := record
			return &rec, nil
		}
	}

	return nil, nil // Record not found
}

// listRecords lists all DNS records for the hosted zone
func (r *Route53Provider) listRecords(ctx context.Context) ([]types.ResourceRecordSet, error) {
	var records []types.ResourceRecordSet

	input := &route53.ListResourceRecordSetsInput{
		HostedZoneId: aws.String(r.config.HostedZoneID),
	}

	for {
		resp, err := r.client.ListResourceRecordSets(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("failed to list resource record sets: %w", err)
		}

		records = append(records, resp.ResourceRecordSets...)

		if !resp.IsTruncated {
			break
		}

		input.StartRecordName = resp.NextRecordName
		input.StartRecordType = resp.NextRecordType
		input.StartRecordIdentifier = resp.NextRecordIdentifier
	}

	return records, nil
}

// updateExistingRecord updates an existing DNS record
func (r *Route53Provider) updateExistingRecord(ctx context.Context, existingRecord *types.ResourceRecordSet, record interfaces.DNSRecord) error {
	// Create new ResourceRecordSet preserving routing properties from existing record
	newRecordSet := &types.ResourceRecordSet{
		Name: aws.String(record.Name),
		Type: types.RRType(record.Type),
		TTL:  aws.Int64(int64(record.TTL)),
		ResourceRecords: []types.ResourceRecord{
			{
				Value: aws.String(record.Value),
			},
		},
	}

	// Preserve routing properties from existing record
	if existingRecord.SetIdentifier != nil {
		newRecordSet.SetIdentifier = existingRecord.SetIdentifier
	}
	if existingRecord.Weight != nil {
		newRecordSet.Weight = existingRecord.Weight
	}
	if existingRecord.HealthCheckId != nil {
		newRecordSet.HealthCheckId = existingRecord.HealthCheckId
	}
	if existingRecord.TrafficPolicyInstanceId != nil {
		newRecordSet.TrafficPolicyInstanceId = existingRecord.TrafficPolicyInstanceId
	}
	if existingRecord.Region != "" {
		newRecordSet.Region = existingRecord.Region
	}
	if existingRecord.Failover != "" {
		newRecordSet.Failover = existingRecord.Failover
	}
	if existingRecord.MultiValueAnswer != nil {
		newRecordSet.MultiValueAnswer = existingRecord.MultiValueAnswer
	}

	change := types.Change{
		Action:            types.ChangeActionUpsert,
		ResourceRecordSet: newRecordSet,
	}

	input := &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(r.config.HostedZoneID),
		ChangeBatch: &types.ChangeBatch{
			Changes: []types.Change{change},
		},
	}

	_, err := r.client.ChangeResourceRecordSets(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to update resource record set: %w", err)
	}

	r.logger.Info("DNS record updated successfully",
		zap.String("provider", "route53"),
		zap.String("record", record.Name),
	)

	return nil
}

// createNewRecord creates a new DNS record
func (r *Route53Provider) createNewRecord(ctx context.Context, record interfaces.DNSRecord) error {
	change := types.Change{
		Action: types.ChangeActionCreate,
		ResourceRecordSet: &types.ResourceRecordSet{
			Name: aws.String(record.Name),
			Type: types.RRType(record.Type),
			TTL:  aws.Int64(int64(record.TTL)),
			ResourceRecords: []types.ResourceRecord{
				{
					Value: aws.String(record.Value),
				},
			},
		},
	}

	input := &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(r.config.HostedZoneID),
		ChangeBatch: &types.ChangeBatch{
			Changes: []types.Change{change},
		},
	}

	_, err := r.client.ChangeResourceRecordSets(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to create resource record set: %w", err)
	}

	r.logger.Info("DNS record created successfully",
		zap.String("provider", "route53"),
		zap.String("record", record.Name),
	)

	return nil
}

// deleteRecord deletes a DNS record
func (r *Route53Provider) deleteRecord(ctx context.Context, record *types.ResourceRecordSet) error {
	change := types.Change{
		Action:            types.ChangeActionDelete,
		ResourceRecordSet: record,
	}

	input := &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(r.config.HostedZoneID),
		ChangeBatch: &types.ChangeBatch{
			Changes: []types.Change{change},
		},
	}

	_, err := r.client.ChangeResourceRecordSets(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to delete resource record set: %w", err)
	}

	r.logger.Info("DNS record deleted successfully",
		zap.String("provider", "route53"),
		zap.String("record", *record.Name),
	)

	return nil
}
