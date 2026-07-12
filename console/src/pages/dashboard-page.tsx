import { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { ActiveAlertsSection } from "@/components/dashboard/active-alerts-section";
import { MetricGrid } from "@/components/dashboard/metric-grid";
import { MetricHistorySection } from "@/components/dashboard/metric-history-section";
import { ServerOverviewSection } from "@/components/dashboard/server-overview-section";
import { ServerSelector } from "@/components/dashboard/server-selector";
import { TimeRangeSelector } from "@/components/dashboard/time-range-selector";
import { useServer, useServers } from "@/features/servers/hooks";
import { useMetricsRange } from "@/features/metrics/hooks";
import { useServerMetrics } from "@/features/metrics/use-server-metrics";
import type { MetricsRange } from "@/types/system-metrics";

export function DashboardPage() {
	const {
		data: servers = [],
		isLoading: isLoadingServers,
		isError: isServersError,
	} = useServers();
	const [searchParams, setSearchParams] = useSearchParams();
	const [range, setRange] = useState<MetricsRange>("24h");

	const serverId = searchParams.get("server") ?? servers[0]?.id;

	function handleServerChange(nextServerId: string) {
		setSearchParams((params) => {
			params.set("server", nextServerId);
			return params;
		});
	}
	const { latest, isLoading: isLoadingMetrics } = useServerMetrics(serverId);
	const { data: server, isLoading: isLoadingServer } = useServer(serverId);
	const {
		data: rangeData,
		isLoading: isLoadingRange,
		isError: isRangeError,
	} = useMetricsRange(serverId, range);

	if (isServersError) {
		return (
			<p className="text-sm text-destructive">
				Gagal memuat daftar server. Coba muat ulang halaman.
			</p>
		);
	}

	if (!isLoadingServers && servers.length === 0) {
		return (
			<p className="text-sm text-muted-foreground">
				Belum ada server yang terdaftar.
			</p>
		);
	}

	return (
		<div className="space-y-4">
			<ServerSelector
				servers={servers}
				value={serverId}
				onChange={handleServerChange}
				isLoading={isLoadingServers}
			/>
			<ServerOverviewSection server={server} isLoading={isLoadingServer} />

			<MetricGrid latest={latest} isLoading={isLoadingMetrics} />
			<div className="flex items-center justify-between">
				<h2 className="text-sm font-medium">History</h2>
				<TimeRangeSelector value={range} onChange={setRange} />
			</div>
			{isRangeError ? (
				<p className="text-sm text-destructive">
					Gagal memuat data history untuk rentang ini.
				</p>
			) : (
				<MetricHistorySection
					history={rangeData?.data ?? []}
					statistics={rangeData?.statistics}
					isLoading={isLoadingRange}
				/>
			)}
			<ActiveAlertsSection />
		</div>
	);
}
