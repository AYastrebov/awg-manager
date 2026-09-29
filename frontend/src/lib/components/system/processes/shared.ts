export type SortField = 'cpu' | 'mem' | 'pid' | 'name' | 'user' | 'threads' | 'state' | 'time';

export type CpuLevel = 'low' | 'med' | 'high';

// Бэкенд отбрасывает всё после десятых: ядро из трёх, занятое целиком, —
// 33.3 %, а не 33.33 %. Без допуска такое ядро никогда не дошло бы до порога.
const TRUNC_TOLERANCE = 0.1;

// Без cpuCount pct — доля мощности ядра или всего процессора (полосы
// дашборда). С cpuCount pct — доля всего процессора, занятая процессом, и
// пороги считаются в ядрах: ½ ядра — med, целое ядро — high (однопоточный
// процесс выше не поднимется).
export function getCpuClass(pct: number, cpuCount = 0): CpuLevel {
	if (cpuCount > 0) {
		const core = 100 / cpuCount;
		if (pct >= core - TRUNC_TOLERANCE) return 'high';
		if (pct >= core / 2 - TRUNC_TOLERANCE) return 'med';
		return 'low';
	}
	if (pct >= 80) return 'high';
	if (pct >= 45) return 'med';
	return 'low';
}

// Ширина мини-полосы процесса в той же шкале, что и цвет: полная полоса —
// одно ядро целиком. Без cpuCount — доля всего процессора, как есть.
export function cpuBarWidth(pct: number, cpuCount = 0): number {
	return Math.min(100, cpuCount > 0 ? pct * cpuCount : pct);
}
