// Copyright 2021 VMware, Inc.
// SPDX-License-Identifier: Apache License 2.0
package models

// This file is auto-generated.

// BackupConfiguration backup configuration
// swagger:model BackupConfiguration
type BackupConfiguration struct {

	// UNIX time since epoch in microseconds. Units(MICROSECONDS).
	// Read Only: true
	LastModified *string `json:"_last_modified,omitempty"`

	// AWS Access Key ID. Field introduced in 18.2.3. Allowed in Basic edition, Essentials edition, Enterprise edition.
	AwsAccessKey *string `json:"aws_access_key,omitempty"`

	// AWS bucket. Field introduced in 18.2.3. Allowed in Basic edition, Essentials edition, Enterprise edition.
	AwsBucketID *string `json:"aws_bucket_id,omitempty"`

	// AWS Secret Access Key. Field introduced in 18.2.3. Allowed in Basic edition, Essentials edition, Enterprise edition.
	AwsSecretAccess *string `json:"aws_secret_access,omitempty"`

	// Prefix of the exported configuration file. Field introduced in 17.1.1.
	BackupFilePrefix *string `json:"backup_file_prefix,omitempty"`

	// Default passphrase for configuration export and periodic backup.
	BackupPassphrase *string `json:"backup_passphrase,omitempty"`

	// Protobuf versioning for config pbs. Field introduced in 21.1.1.
	ConfigpbAttributes *ConfigPbAttributes `json:"configpb_attributes,omitempty"`

	// Rotate the backup files based on this count. Allowed values are 1-20.
	MaximumBackupsStored *int32 `json:"maximum_backups_stored,omitempty"`

	// Name of backup configuration.
	// Required: true
	Name *string `json:"name"`

	// Directory at remote destination with write permission for ssh user.
	RemoteDirectory *string `json:"remote_directory,omitempty"`

	// Remote file transfer protocol type. Enum options - SCP, SFTP. Field introduced in 22.1.1. Allowed in Basic(Allowed values- SCP,SFTP) edition, Enterprise edition.
	RemoteFileTransferProtocol *string `json:"remote_file_transfer_protocol,omitempty"`

	// Remote Destination.
	RemoteHostname *string `json:"remote_hostname,omitempty"`

	// Local Backup.
	SaveLocal *bool `json:"save_local,omitempty"`

	// Access Credentials for remote destination. It is a reference to an object of type CloudConnectorUser.
	SSHUserRef *string `json:"ssh_user_ref,omitempty"`

	//  It is a reference to an object of type Tenant.
	TenantRef *string `json:"tenant_ref,omitempty"`

	// Remote Backup.
	UploadToRemoteHost *bool `json:"upload_to_remote_host,omitempty"`

	// Cloud Backup. Field introduced in 18.2.3. Allowed in Basic edition, Essentials edition, Enterprise edition.
	UploadToS3 *bool `json:"upload_to_s3,omitempty"`

	// url
	// Read Only: true
	URL *string `json:"url,omitempty"`

	// Unique object identifier of the object.
	UUID *string `json:"uuid,omitempty"`
}
