import { describe, it, expect } from 'vitest';
import { getCpuClass } from './shared';

describe('getCpuClass', () => {
	// Процесс: pct — доля всего процессора, пороги в ядрах.
	it('процесс на 4 ядрах: целое ядро (25 %) — high, половина — med', () => {
		expect(getCpuClass(25, 4)).toBe('high');
		expect(getCpuClass(24.9, 4)).toBe('med');
		expect(getCpuClass(12.5, 4)).toBe('med');
		expect(getCpuClass(12.4, 4)).toBe('low');
	});

	it('процесс на 2 ядрах: пороги 50 % и 25 %', () => {
		expect(getCpuClass(50, 2)).toBe('high');
		expect(getCpuClass(25, 2)).toBe('med');
		expect(getCpuClass(24, 2)).toBe('low');
	});

	// Полосы дашборда: pct — доля мощности, пороги прежние.
	it('без числа ядер — пороги 45 и 80', () => {
		expect(getCpuClass(80)).toBe('high');
		expect(getCpuClass(45)).toBe('med');
		expect(getCpuClass(44)).toBe('low');
	});
});
