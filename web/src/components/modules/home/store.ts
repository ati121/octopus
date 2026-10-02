'use client';

import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';

export type RankSortMode = 'count' | 'tokens';
export type ChartPeriod = '1' | '7' | '30' | 'all';

interface HomeViewState {
    rankSortMode: RankSortMode;
    chartPeriod: ChartPeriod;
    setRankSortMode: (value: RankSortMode) => void;
    setChartPeriod: (value: ChartPeriod) => void;
}

export const useHomeViewStore = create<HomeViewState>()(
    persist(
        (set) => ({
            rankSortMode: 'tokens',
            chartPeriod: '7',
            setRankSortMode: (value) => set({ rankSortMode: value }),
            setChartPeriod: (value) => set({ chartPeriod: value }),
        }),
        {
            name: 'home-view-options-storage',
            storage: createJSONStorage(() => localStorage),
            merge: (persisted, current) => {
                const saved = persisted as Partial<HomeViewState> | undefined;
                return {
                    ...current,
                    chartPeriod: saved?.chartPeriod ?? current.chartPeriod,
                    rankSortMode: saved?.rankSortMode === 'count' ? 'count' : 'tokens',
                };
            },
            partialize: (state) => ({
                rankSortMode: state.rankSortMode,
                chartPeriod: state.chartPeriod,
            }),
        }
    )
);
