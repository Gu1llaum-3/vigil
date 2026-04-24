export interface OSInfo {
	family: string
	name: string
	version: string
}

export interface ResourceInfo {
	cpu_model: string
	cpu_cores: number
	ram_mb: number
	swap_mb: number
}

export interface NetworkInfo {
	gateway: string
	dns_servers: string[]
}

export interface StorageMount {
	device: string
	mountpoint: string
	fs_type: string
	total_bytes: number
	used_bytes: number
	available_bytes: number
	used_percent: number
}

export interface OutdatedPackage {
	name: string
	installed_version: string
	candidate_version: string
	is_security: boolean
}

export interface PackageInfo {
	installed_count: number
	outdated_count: number
	security_count: number
	last_upgrade_at: string
	last_upgrade_age_days: number
	last_upgrade_known: boolean
	outdated: OutdatedPackage[]
}

export interface RepositoryInfo {
	name: string
	url: string
	enabled: boolean
	secure: boolean
	distribution: string
	components: string
}

export interface RebootInfo {
	required: boolean
	reason: string
}

export interface ContainerInfo {
	id: string
	name: string
	image: string
	image_ref: string
	image_id: string
	repo_digests: string[]
	current_ref_image_id: string
	current_ref_repo_digests: string[]
	status: string
	status_text: string
	ports: string
	exit_code?: number | null
}

export interface DockerInfo {
	state: string
	container_count: number
	running_count: number
	containers: ContainerInfo[]
}

export interface ContainerImageAudit {
	status: string
	policy: string
	registry: string
	repository: string
	tag: string
	current_ref: string
	local_image_id: string
	local_digest: string
	latest_image_id: string
	latest_tag: string
	latest_digest: string
	line_status?: string
	line_latest_tag?: string
	same_major_latest_tag?: string
	overall_latest_tag?: string
	new_major_tag?: string
	major_update_available?: boolean
	checked_at: string
	error?: string
}

export interface HostSnapshot {
	hostname: string
	primary_ip: string
	os: OSInfo
	kernel: string
	architecture: string
	uptime_seconds: number
	resources: ResourceInfo
	network: NetworkInfo
	storage: StorageMount[]
	packages: PackageInfo
	repositories: RepositoryInfo[]
	reboot: RebootInfo
	docker: DockerInfo
	collected_at: string
}

export interface DashboardHost extends HostSnapshot {
	id: string
	name: string
	status: string
	last_seen: string
}

export interface DistributionEntry {
	label: string
	value: number
}

export interface DashboardSummary {
	total_hosts: number
	connected_hosts: number
	offline_hosts: number
	total_monitors: number
	up_monitors: number
	hosts_needing_updates: number
	hosts_needing_reboot: number
	total_outdated_packages: number
	total_security_updates: number
	total_containers: number
	running_containers: number
	containers_in_warning: number
	containers_in_error: number
	containers_with_image_updates: number
	insecure_repositories: number
	os_distribution: DistributionEntry[]
	update_status_distribution: DistributionEntry[]
}

export interface PackageAggregate {
	name: string
	affected_hosts: number
	security_hosts: number
}

export interface RepositoryAggregate {
	name: string
	url: string
	secure: boolean
	enabled_hosts: number
}

export interface ContainerFleetEntry extends ContainerInfo {
	host_id: string
	host_name: string
	host_ip: string
	image_audit?: ContainerImageAudit | null
}

export interface DashboardResponse {
	summary: DashboardSummary
	hosts: DashboardHost[]
	packages: PackageAggregate[]
	repositories: RepositoryAggregate[]
	containers: ContainerFleetEntry[]
}
