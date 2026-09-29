import { describe, it, expect } from 'vitest';
import { cpuBarWidth, getCpuClass } from './shared';

describe('getCpuClass', () => {
	// Процесс: pct — доля всего процессора, пороги в ядрах.
	it('процесс на 4 ядрах: целое ядро (25 %) — high, половина — med', () => {
		expect(getCpuClass(25, 4)).toBe('high');
		expect(getCpuClass(24.9, 4)).toBe('high'); // допуск на усечение до десятых
		expect(getCpuClass(24.8, 4)).toBe('med');
		expect(getCpuClass(12.5, 4)).toBe('med');
		expect(getCpuClass(12.3, 4)).toBe('low');
	});

	it('процесс на 2 ядрах: пороги 50 % и 25 %', () => {
		expect(getCpuClass(50, 2)).toBe('high');
		expect(getCpuClass(25, 2)).toBe('med');
		expect(getCpuClass(24, 2)).toBe('low');
	});

	// Ядро из трёх, занятое целиком, приходит усечённым до 33.3 % (33.33…).
	it('процесс на 3 ядрах: усечённое целое ядро — high, половина — med', () => {
		expect(getCpuClass(33.3, 3)).toBe('high');
		expect(getCpuClass(33.1, 3)).toBe('med');
		expect(getCpuClass(16.6, 3)).toBe('med');
		expect(getCpuClass(16.4, 3)).toBe('low');
	});

	// Полосы дашборда: pct — доля мощности, пороги прежние.
	it('без числа ядер — пороги 45 и 80', () => {
		expect(getCpuClass(80)).toBe('high');
		expect(getCpuClass(45)).toBe('med');
		expect(getCpuClass(44)).toBe('low');
	});
});

describe('cpuBarWidth', () => {
	it('полная полоса — одно ядро целиком', () => {
		expect(cpuBarWidth(25, 4)).toBe(100);
		expect(cpuBarWidth(12.5, 4)).toBe(50);
		expect(cpuBarWidth(60, 4)).toBe(100);
	});

	it('без числа ядер — доля как есть', () => {
		expect(cpuBarWidth(30)).toBe(30);
	});
});
