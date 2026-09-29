export type SortField = 'cpu' | 'mem' | 'pid' | 'name' | 'user' | 'threads' | 'state' | 'time';

export type CpuLevel = 'low' | 'med' | 'high';

// Без cpuCount pct — доля мощности ядра или всего процессора (полосы
// дашборда). С cpuCount pct — доля всего процессора, занятая процессом, и
// пороги считаются в ядрах: ½ ядра — med, целое ядро — high (однопоточный
// процесс выше не поднимется).
export function getCpuClass(pct: number, cpuCount = 0): CpuLevel {
	if (cpuCount > 0) {
		const core = 100 / cpuCount;
		if (pct >= core) return 'high';
		if (pct >= core / 2) return 'med';
		return 'low';
	}
	if (pct >= 80) return 'high';
	if (pct >= 45) return 'med';
	return 'low';
}
