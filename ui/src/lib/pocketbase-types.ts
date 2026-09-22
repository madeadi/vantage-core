/**
* This file was @generated using pocketbase-typegen
*/

import type PocketBase from 'pocketbase'
import type { RecordService } from 'pocketbase'

export const Collections = {
	Authorigins: "_authOrigins",
	Externalauths: "_externalAuths",
	Mfas: "_mfas",
	Otps: "_otps",
	Superusers: "_superusers",
	AgentEvents: "agent_events",
	AgentGroups: "agent_groups",
	AgentLayouts: "agent_layouts",
	Agents: "agents",
	Layouts: "layouts",
	Missions: "missions",
	TelemetrySettings: "telemetry_settings",
	Users: "users",
} as const
export type Collections = typeof Collections[keyof typeof Collections]

// Alias types for improved usability
export type IsoDateString = string
export type IsoAutoDateString = string & { readonly autodate: unique symbol }
export type RecordIdString = string
export type FileNameString = string & { readonly filename: unique symbol }
export type HTMLString = string

type ExpandType<T> = unknown extends T
	? T extends unknown
		? { expand?: unknown }
		: { expand: T }
	: { expand: T }

// System fields
export type BaseSystemFields<T = unknown> = {
	id: RecordIdString
	collectionId: string
	collectionName: Collections
} & ExpandType<T>

export type AuthSystemFields<T = unknown> = {
	email: string
	emailVisibility: boolean
	username: string
	verified: boolean
} & BaseSystemFields<T>

// Record types for each collection

export type AuthoriginsRecord = {
	collectionRef: string
	created: IsoAutoDateString
	fingerprint: string
	id: string
	recordRef: string
	updated: IsoAutoDateString
}

export type ExternalauthsRecord = {
	collectionRef: string
	created: IsoAutoDateString
	id: string
	provider: string
	providerId: string
	recordRef: string
	updated: IsoAutoDateString
}

export type MfasRecord = {
	collectionRef: string
	created: IsoAutoDateString
	id: string
	method: string
	recordRef: string
	updated: IsoAutoDateString
}

export type OtpsRecord = {
	collectionRef: string
	created: IsoAutoDateString
	id: string
	password: string
	recordRef: string
	sentTo?: string
	updated: IsoAutoDateString
}

export type SuperusersRecord = {
	created: IsoAutoDateString
	email: string
	emailVisibility?: boolean
	id: string
	password: string
	tokenKey: string
	updated: IsoAutoDateString
	verified?: boolean
}

export const AgentEventsSeverityOptions = {
	"info": "info",
	"warning": "warning",
	"error": "error",
} as const
export type AgentEventsSeverityOptions = typeof AgentEventsSeverityOptions[keyof typeof AgentEventsSeverityOptions]
export type AgentEventsRecord<Tsample = unknown> = {
	agent_id: string
	count: number
	detail?: string
	first_seen: IsoDateString
	id: string
	kind?: string
	last_seen: IsoDateString
	sample?: null | Tsample
	severity: AgentEventsSeverityOptions
	signature?: string
	type: string
}

export type AgentGroupsRecord<Ttelemetry_mapping = unknown, Ttelemetry_schema = unknown> = {
	description?: string
	id: string
	name: string
	telemetry_mapping?: null | Ttelemetry_mapping
	telemetry_schema?: null | Ttelemetry_schema
}

export type AgentLayoutsRecord<Ttransformation_matrix = unknown> = {
	agent_id: string
	id: string
	layout_id: string
	north_offset?: number
	transformation_matrix?: null | Ttransformation_matrix
}

export type AgentsRecord = {
	agent_group?: RecordIdString
	agent_id: string
	id: string
	key: string
	name?: string
}

export const LayoutsCoordinateSystemOptions = {
	"pixel": "pixel",
	"latlon": "latlon",
} as const
export type LayoutsCoordinateSystemOptions = typeof LayoutsCoordinateSystemOptions[keyof typeof LayoutsCoordinateSystemOptions]
export type LayoutsRecord<Tgis_bound = unknown> = {
	coordinate_system?: LayoutsCoordinateSystemOptions
	gis_bound?: null | Tgis_bound
	id: string
	layout_id?: string
	name?: string
	pixel_file?: FileNameString
}

export type MissionsRecord = {
	id: string
	key: string
	mission_id: string
	name?: string
}

export type TelemetrySettingsRecord = {
	agent_group: RecordIdString
	id: string
	persist_enabled?: boolean
}

export const UsersRoleOptions = {
	"admin": "admin",
	"operator": "operator",
	"viewer": "viewer",
} as const
export type UsersRoleOptions = typeof UsersRoleOptions[keyof typeof UsersRoleOptions]
export type UsersRecord = {
	avatar?: FileNameString
	created: IsoAutoDateString
	email: string
	emailVisibility?: boolean
	id: string
	name?: string
	password: string
	role: UsersRoleOptions
	tokenKey: string
	updated: IsoAutoDateString
	username: string
	verified?: boolean
}

// Response types include system fields and match responses from the PocketBase API
export type AuthoriginsResponse<Texpand = unknown> = Required<AuthoriginsRecord> & BaseSystemFields<Texpand>
export type ExternalauthsResponse<Texpand = unknown> = Required<ExternalauthsRecord> & BaseSystemFields<Texpand>
export type MfasResponse<Texpand = unknown> = Required<MfasRecord> & BaseSystemFields<Texpand>
export type OtpsResponse<Texpand = unknown> = Required<OtpsRecord> & BaseSystemFields<Texpand>
export type SuperusersResponse<Texpand = unknown> = Required<SuperusersRecord> & AuthSystemFields<Texpand>
export type AgentEventsResponse<Tsample = unknown, Texpand = unknown> = Required<AgentEventsRecord<Tsample>> & BaseSystemFields<Texpand>
export type AgentGroupsResponse<Ttelemetry_mapping = unknown, Ttelemetry_schema = unknown, Texpand = unknown> = Required<AgentGroupsRecord<Ttelemetry_mapping, Ttelemetry_schema>> & BaseSystemFields<Texpand>
export type AgentLayoutsResponse<Ttransformation_matrix = unknown, Texpand = unknown> = Required<AgentLayoutsRecord<Ttransformation_matrix>> & BaseSystemFields<Texpand>
export type AgentsResponse<Texpand = unknown> = Required<AgentsRecord> & BaseSystemFields<Texpand>
export type LayoutsResponse<Tgis_bound = unknown, Texpand = unknown> = Required<LayoutsRecord<Tgis_bound>> & BaseSystemFields<Texpand>
export type MissionsResponse<Texpand = unknown> = Required<MissionsRecord> & BaseSystemFields<Texpand>
export type TelemetrySettingsResponse<Texpand = unknown> = Required<TelemetrySettingsRecord> & BaseSystemFields<Texpand>
export type UsersResponse<Texpand = unknown> = Required<UsersRecord> & AuthSystemFields<Texpand>

// Types containing all Records and Responses, useful for creating typing helper functions

export type CollectionRecords = {
	_authOrigins: AuthoriginsRecord
	_externalAuths: ExternalauthsRecord
	_mfas: MfasRecord
	_otps: OtpsRecord
	_superusers: SuperusersRecord
	agent_events: AgentEventsRecord
	agent_groups: AgentGroupsRecord
	agent_layouts: AgentLayoutsRecord
	agents: AgentsRecord
	layouts: LayoutsRecord
	missions: MissionsRecord
	telemetry_settings: TelemetrySettingsRecord
	users: UsersRecord
}

export type CollectionResponses = {
	_authOrigins: AuthoriginsResponse
	_externalAuths: ExternalauthsResponse
	_mfas: MfasResponse
	_otps: OtpsResponse
	_superusers: SuperusersResponse
	agent_events: AgentEventsResponse
	agent_groups: AgentGroupsResponse
	agent_layouts: AgentLayoutsResponse
	agents: AgentsResponse
	layouts: LayoutsResponse
	missions: MissionsResponse
	telemetry_settings: TelemetrySettingsResponse
	users: UsersResponse
}

// Utility types for create/update operations

type ProcessCreateAndUpdateFields<T> = Omit<{
	// Omit AutoDate fields
	[K in keyof T as Extract<T[K], IsoAutoDateString> extends never ? K : never]: 
		// Convert FileNameString to File
		T[K] extends infer U ? 
			U extends (FileNameString | FileNameString[]) ? 
				U extends any[] ? File[] : File 
			: U
		: never
}, 'id'>

// Create type for Auth collections
export type CreateAuth<T> = {
	id?: RecordIdString
	email: string
	emailVisibility?: boolean
	password: string
	passwordConfirm: string
	verified?: boolean
} & ProcessCreateAndUpdateFields<T>

// Create type for Base collections
export type CreateBase<T> = {
	id?: RecordIdString
} & ProcessCreateAndUpdateFields<T>

// Update type for Auth collections
export type UpdateAuth<T> = Partial<
	Omit<ProcessCreateAndUpdateFields<T>, keyof AuthSystemFields>
> & {
	email?: string
	emailVisibility?: boolean
	oldPassword?: string
	password?: string
	passwordConfirm?: string
	verified?: boolean
}

// Update type for Base collections
export type UpdateBase<T> = Partial<
	Omit<ProcessCreateAndUpdateFields<T>, keyof BaseSystemFields>
>

// Get the correct create type for any collection
export type Create<T extends keyof CollectionResponses> =
	CollectionResponses[T] extends AuthSystemFields
		? CreateAuth<CollectionRecords[T]>
		: CreateBase<CollectionRecords[T]>

// Get the correct update type for any collection
export type Update<T extends keyof CollectionResponses> =
	CollectionResponses[T] extends AuthSystemFields
		? UpdateAuth<CollectionRecords[T]>
		: UpdateBase<CollectionRecords[T]>

// Type for usage with type asserted PocketBase instance
// https://github.com/pocketbase/js-sdk#specify-typescript-definitions

export type TypedPocketBase = {
	collection<T extends keyof CollectionResponses>(
		idOrName: T
	): RecordService<CollectionResponses[T]>
} & PocketBase
