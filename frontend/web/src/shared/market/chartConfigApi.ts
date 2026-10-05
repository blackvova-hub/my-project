import { http } from '../api/http';
import { normalizeDrawingSets, type DrawingSets } from './chartDrawings';
import { isIndicatorId, type IndicatorId } from './indicatorTypes';
import type { PerpInterval } from './perpMarketData';

export type ChartConfig = {
  version: 1;
  selected_interval: PerpInterval;
  active_indicator_ids: IndicatorId[];
  drawing_sets: DrawingSets;
};

type ChartConfigResponse = {
  config: {
    version: number;
    selected_interval: string;
    active_indicator_ids: string[];
    drawing_sets?: unknown;
  };
  saved: boolean;
  updated_at?: string;
};

const INTERVALS = new Set<PerpInterval>(['1m', '5m', '15m', '1h', '4h']);

function normalizeConfig(response: ChartConfigResponse): ChartConfig {
  const selectedInterval = INTERVALS.has(response.config.selected_interval as PerpInterval)
    ? response.config.selected_interval as PerpInterval
    : '1m';
  return {
    version: 1,
    selected_interval: selectedInterval,
    active_indicator_ids: response.config.active_indicator_ids.filter(isIndicatorId),
    drawing_sets: normalizeDrawingSets(response.config.drawing_sets),
  };
}

export async function fetchChartConfig(signal?: AbortSignal) {
  return normalizeConfig(await http<ChartConfigResponse>('/chart/config', { signal }));
}

export async function saveChartConfig(config: ChartConfig) {
  return normalizeConfig(await http<ChartConfigResponse>('/chart/config', {
    method: 'PUT',
    body: JSON.stringify(config),
  }));
}
