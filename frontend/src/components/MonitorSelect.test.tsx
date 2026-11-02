// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { MonitorSelect } from './MonitorSelect';

const options = Array.from({ length: 30 }, (_, index) => ({ value: String(index), label: `选项 ${index + 1}` }));

afterEach(() => {
	cleanup();
	vi.restoreAllMocks();
});

it('keeps a long menu open while scrolling its options, and closes it when the page scrolls', () => {
	const onChange = vi.fn();
	render(<MonitorSelect value="0" onChange={onChange} options={options} ariaLabel="字段" />);
	fireEvent.click(screen.getByRole('button', { name: '字段' }));
	fireEvent.scroll(screen.getByRole('listbox'));
	fireEvent.click(screen.getByRole('option', { name: '选项 30' }));
	expect(onChange).toHaveBeenCalledWith('29');
	expect(screen.queryByRole('listbox')).toBeNull();

	fireEvent.click(screen.getByRole('button', { name: '字段' }));
	fireEvent.scroll(window);
	expect(screen.queryByRole('listbox')).toBeNull();
});

it('scrolls to an option reached with the keyboard without dismissing the menu', () => {
	const onChange = vi.fn();
	render(<MonitorSelect value="0" onChange={onChange} options={options} ariaLabel="字段" />);
	fireEvent.click(screen.getByRole('button', { name: '字段' }));
	const menu = screen.getByRole('listbox');
	Object.defineProperty(menu, 'clientHeight', { value: 100 });
	Array.from(menu.children).forEach((option, index) => {
		Object.defineProperties(option, { offsetTop: { value: index * 31 }, offsetHeight: { value: 30 } });
	});
	for (let index = 0; index < 5; index++) fireEvent.keyDown(document, { key: 'ArrowDown' });
	expect(menu.scrollTop).toBe(85);
	fireEvent.scroll(menu);
	expect(screen.getByRole('listbox')).toBe(menu);
	fireEvent.keyDown(document, { key: 'Enter' });
	expect(onChange).toHaveBeenCalledWith('5');
});

it('fits a long menu inside a short rule editor when neither side has room for the full list', () => {
	vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
		const top = this.classList.contains('monitor-editor') ? 200 : 300;
		const bottom = this.classList.contains('monitor-editor') ? 460 : 332;
		return { top, bottom, left: 100, right: 300, width: 200, height: bottom - top, x: 100, y: top, toJSON: () => ({}) };
	});
	render(<div className="monitor-editor"><MonitorSelect value="0" onChange={() => {}} options={options} ariaLabel="字段" /></div>);
	fireEvent.click(screen.getByRole('button', { name: '字段' }));
	const menu = screen.getByRole('listbox');
	expect(menu.style.top).toBe('338px');
	expect(menu.style.maxHeight).toBe('114px');
	fireEvent.keyDown(document, { key: 'Escape' });
	expect(screen.queryByRole('listbox')).toBeNull();
});
