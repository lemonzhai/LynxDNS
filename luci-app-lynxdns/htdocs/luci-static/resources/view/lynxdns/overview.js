'use strict';
'require view';
'require ui';
'require uci';

var apiBase;

function apiCall(method, path, data) {
	return new Promise(function(resolve) {
		var xhr = new XMLHttpRequest();
		xhr.open(method, apiBase + path, true);
		if (data) xhr.setRequestHeader('Content-Type', 'application/json');
		xhr.onreadystatechange = function() {
			if (xhr.readyState === 4) {
				try { resolve(JSON.parse(xhr.responseText || '{}')); }
				catch (e) { resolve({ code: 500, message: '解析错误' }); }
			}
		};
		xhr.send(data ? JSON.stringify(data) : null);
	});
}

function apiGet(path) { return apiCall('GET', path, null); }
function apiPost(path, data) { return apiCall('POST', path, data); }
function apiPatch(path, data) { return apiCall('PATCH', path, data); }

function showToast(msg) {
	var existing = document.querySelectorAll('.lynxdns-toast');
	existing.forEach(function(el) { el.remove(); });
	var t = E('div', { 'class': 'lynxdns-toast' }, msg);
	document.body.appendChild(t);
	setTimeout(function() { if (t.parentNode) t.remove(); }, 2500);
}

function fieldRow(label, content, description) {
	var children = (typeof content === 'string') ? [E('span', {}, content)] : [content];
	if (description) children.push(E('div', { 'class': 'cbi-value-description' }, description));
	return E('div', { 'class': 'cbi-value' }, [
		E('label', { 'class': 'cbi-value-title' }, label),
		E('div', { 'class': 'cbi-value-field' }, children)
	]);
}

function formatUptime(sec) {
	var parts = [];
	if (sec >= 86400) { parts.push(Math.floor(sec / 86400) + ' 天'); sec %= 86400; }
	if (sec >= 3600) { parts.push(Math.floor(sec / 3600) + ' 时'); sec %= 3600; }
	if (sec >= 60) { parts.push(Math.floor(sec / 60) + ' 分'); sec %= 60; }
	parts.push(sec + ' 秒');
	return parts.join(' ');
}

function createStatCard(title, value, color) {
	return E('div', {
		'style': 'background:#f8f9fa;border:1px solid #e0e0e0;border-radius:8px;' +
			'padding:12px;text-align:center;border-left:3px solid ' + color
	}, [
		E('div', { 'style': 'font-size:11px;color:#888;margin-bottom:4px' }, title),
		E('div', { 'style': 'font-size:18px;font-weight:bold;color:' + color }, value)
	]);
}

function renderOverviewStats(container, s, running) {
	while (container.firstChild) container.removeChild(container.firstChild);
	if (!s) {
		container.appendChild(E('div', { 'class': 'cbi-section-descr' },
			'[!] 无法加载统计数据。'));
		return;
	}

	var cardsRow = E('div', {
		'style': 'display:grid;grid-template-columns:repeat(auto-fit,minmax(140px,1fr));gap:12px;margin-bottom:12px'
	});
	var statusColor = running ? '#009900' : '#cc0000';
	var statusText = running ? '● 运行中' : '● 已停止';
	cardsRow.appendChild(createStatCard('运行状态', statusText, statusColor));
	cardsRow.appendChild(createStatCard('总查询', String(s.total_queries || 0), '#4a90d9'));
	cardsRow.appendChild(createStatCard('缓存命中', (s.cache_hit_rate || 0).toFixed(1) + '%', '#5cb85c'));
	cardsRow.appendChild(createStatCard('平均延迟', (s.avg_latency_ms || 0).toFixed(1) + ' ms', '#f0ad4e'));
	cardsRow.appendChild(createStatCard('每秒查询', (s.queries_per_second || 0).toFixed(1), '#5bc0de'));
	cardsRow.appendChild(createStatCard('已拦截', String(s.blocked_queries || 0), '#d9534f'));
	cardsRow.appendChild(createStatCard('已重定向', String(s.redirected_queries || 0), '#9b59b6'));
	container.appendChild(cardsRow);

	if (s.by_action) {
		var actionColors = {
			domestic: '#5cb85c', remote: '#4a90d9', blocked: '#d9534f',
			redirected: '#9b59b6', default: '#777', cache_hit: '#f0ad4e',
			leak_blocked: '#e67e22'
		};
		var actionLabels = {
			domestic: '国内', remote: '远程', blocked: '拦截',
			redirected: '重定向', default: '常规解析', cache_hit: '缓存',
			leak_blocked: '防泄漏'
		};

		var total = 0;
		Object.keys(s.by_action).forEach(function(key) {
			total += (s.by_action[key] || 0);
		});

		if (total > 0) {
			var segments = [];
			Object.keys(s.by_action).forEach(function(key) {
				var count = s.by_action[key] || 0;
				var pct = (count / total * 100);
				var color = actionColors[key] || '#777';
				var label = actionLabels[key] || key;
				segments.push(E('div', {
					'style': 'width:' + pct + '%;height:100%;background:' + color +
						';transition:width 0.5s;position:relative;display:flex;align-items:center;justify-content:center' +
						(pct > 8 ? '' : '')
				}, pct > 8 ? [E('span', {
					'style': 'font-size:10px;color:#fff;line-height:22px;white-space:nowrap'
				}, label)] : []));
			});
			container.appendChild(E('div', { 'style': 'margin-bottom:12px' }, [
				E('div', { 'style': 'display:flex;justify-content:space-between;font-size:12px;margin-bottom:4px' }, [
					E('span', { 'style': 'color:#666' }, '查询分布'),
					E('span', { 'style': 'font-weight:bold;color:#333' }, '共 ' + total + ' 次')
				]),
				E('div', { 'style': 'display:flex;background:#e0e0e0;border-radius:4px;height:22px;overflow:hidden' }, segments)
			]));
		}

		var badges = [];
		Object.keys(s.by_action).forEach(function(key) {
			var color = actionColors[key] || '#777';
			var label = actionLabels[key] || key;
			badges.push(E('span', {
				'style': 'display:inline-block;padding:2px 10px;margin:2px;border-radius:12px;' +
					'font-size:12px;color:#fff;background:' + color
			}, label + ': ' + s.by_action[key]));
		});
		container.appendChild(E('div', { 'style': 'margin-top:8px;display:flex;flex-wrap:wrap;gap:4px' }, badges));
	}
}

function renderDnsServerStatus(container, st) {
	while (container.firstChild) container.removeChild(container.firstChild);
	if (!st || !st.dns_servers) {
		container.appendChild(E('div', { 'style': 'color:#888;font-size:12px;padding:4px 0' },
			'服务未运行或无法获取服务器状态。'));
		return;
	}

	var table = E('table', {
		'style': 'width:100%;table-layout:fixed;border-collapse:collapse;margin-top:8px'
	}, [
		E('thead', {}, [E('tr', {}, [
			E('th', { 'style': 'width:15%;text-align:left;padding:6px 10px;border-bottom:2px solid #ddd' }, '类型'),
			E('th', { 'style': 'width:45%;text-align:left;padding:6px 10px;border-bottom:2px solid #ddd' }, '地址'),
			E('th', { 'style': 'width:20%;text-align:left;padding:6px 10px;border-bottom:2px solid #ddd' }, '状态'),
			E('th', { 'style': 'width:20%;text-align:left;padding:6px 10px;border-bottom:2px solid #ddd' }, '平均延迟')
		])])
	]);
	var tbody = E('tbody');

	Object.keys(st.dns_servers).forEach(function(group) {
		var servers = st.dns_servers[group];
		var groupLabel = (group === 'domestic') ? '国内'
			: (group === 'remote') ? '远程'
			: group;
		servers.forEach(function(srv) {
			var isUp = srv.status === 'up';
			tbody.appendChild(E('tr', {
				'style': 'border-bottom:1px solid #eee'
			}, [
				E('td', { 'style': 'padding:6px 10px' }, groupLabel),
				E('td', { 'style': 'padding:6px 10px;word-break:break-all' }, srv.address),
				E('td', { 'style': 'padding:6px 10px' }, E('span', {
					'style': 'color:' + (isUp ? '#009900' : '#cc0000') +
						';font-weight:bold'
				}, isUp ? '[正常]' : '[故障]')),
				E('td', { 'style': 'padding:6px 10px' }, isUp
					? (srv.avg_latency_ms || 0).toFixed(1) + ' ms'
					: '-')
			]));
		});
	});

	table.appendChild(tbody);
	container.appendChild(E('div', { 'style': 'font-size:13px;font-weight:bold;margin-bottom:4px' }, 'DNS 服务器状态'));
	container.appendChild(table);
}

return view.extend({
	load: function() {
		apiBase = L.url('admin/services/lynxdns/api');
		return Promise.all([
			uci.load('lynxdns'),
			apiGet('/status').catch(function() { return { code: 500 }; }),
			apiGet('/version').catch(function() { return { code: 500 }; }),
			apiGet('/config').catch(function() { return { code: 500 }; }),
			apiGet('/panel_info').catch(function() { return { code: 500 }; }),
			apiGet('/dns_stats').catch(function() { return { code: 500 }; })
		]);
	},

	render: function(data) {
		if (!document.querySelector('link[href*="lynxdns.css"]')) {
			document.head.appendChild(E('link', { 'rel': 'stylesheet', 'type': 'text/css', 'href': '/luci-static/lynxdns.css' }));
		}
		var statusResp = data[1] || {};
		var versionResp = data[2] || {};
		var configResp = data[3] || {};
		var panelResp = data[4] || {};
		var statsResp = data[5] || {};

		var status = (statusResp.code === 0) ? statusResp.data : null;
		var version = (versionResp.code === 0) ? versionResp.data : null;
		var config = (configResp.code === 0) ? configResp.data : null;
		var panelInfo = (panelResp.code === 0) ? panelResp.data : null;
		var stats = (statsResp.code === 0) ? statsResp.data : null;
		var running = status && status.running;
		var enabled = uci.get('lynxdns', 'main', 'enabled') === '1';

		var m = E('div', { 'class': 'cbi-map' }, [
			E('div', { 'class': 'cbi-map-descr' }, 'DNS 智能分流解析器，智能路由')
		]);

		var statsSection = E('fieldset', { 'class': 'cbi-section' }, [
			E('legend', {}, '查询统计')
		]);
		var statsContainer = E('div', { 'id': 'stats-container', 'style': 'padding:20px;margin-top:0;margin-bottom:0' });
		statsSection.appendChild(statsContainer);
		renderOverviewStats(statsContainer, stats, running);
		m.appendChild(statsSection);

		var statusSection = E('fieldset', { 'class': 'cbi-section' }, [
			E('legend', {}, '服务状态')
		]);
		if (running && status) {
			statusSection.appendChild(fieldRow('运行时长', formatUptime(status.uptime_seconds || 0)));
			statusSection.appendChild(fieldRow('内存占用', (status.memory_mb || 0).toFixed(1) + ' MB'));
		}
		if (version) {
			statusSection.appendChild(fieldRow('内核版本', version.version || 'N/A'));
		}
		if (panelInfo) {
			statusSection.appendChild(fieldRow('面板版本', panelInfo.version || 'N/A'));
		}
		if (config && config.server) {
			statusSection.appendChild(fieldRow('监听地址', config.server.listen_addr || 'N/A'));
			statusSection.appendChild(fieldRow('监听端口', String(config.server.listen_port || 'N/A')));
		}
		m.appendChild(statusSection);

		var ctrlSection = E('fieldset', { 'class': 'cbi-section' }, [
			E('legend', {}, '服务控制')
		]);
		var enabledSwitch = E('input', { 'type': 'checkbox', 'id': 'cb-enabled' });
		if (enabled) enabledSwitch.checked = true;
		ctrlSection.appendChild(fieldRow('启用 LynxDNS', enabledSwitch));

		var restartBtn = E('button', {
			'class': 'cbi-button cbi-button-apply',
			'type': 'button'
		}, '重启服务');
		restartBtn.addEventListener('click', function() {
			apiPost('/restart').then(function(resp) {
				showToast(resp.code === 0 ? '服务正在重启...' : '重启失败：' + (resp.message || ''));
			});
		});

		var stopBtn = E('button', {
			'class': 'cbi-button cbi-button-remove',
			'type': 'button',
			'style': 'margin-left:8px'
		}, '停止服务');
		stopBtn.addEventListener('click', function() {
			if (!confirm('确定要停止 LynxDNS 服务吗？')) return;
			apiPost('/service', { enabled: false }).then(function(resp) {
				showToast(resp.code === 0 ? 'LynxDNS 服务已停止。' : '停止失败：' + (resp.message || ''));
			});
		});

		var btnGroup = E('div', { 'style': 'display:flex;gap:8px;flex-wrap:wrap' }, [restartBtn, stopBtn]);
		ctrlSection.appendChild(fieldRow('服务操作', btnGroup));

		var dnsServerContainer = E('div', { 'id': 'dns-server-container', 'style': 'margin-top:16px' });
		renderDnsServerStatus(dnsServerContainer, status);
		ctrlSection.appendChild(dnsServerContainer);

		m.appendChild(ctrlSection);

		var refreshTimer = setInterval(function() {
			Promise.all([
				apiGet('/dns_stats'),
				apiGet('/status')
			]).then(function(responses) {
				var s = (responses[0] && responses[0].code === 0) ? responses[0].data : null;
				var st = (responses[1] && responses[1].code === 0) ? responses[1].data : null;
				var r = st && st.running;
				renderOverviewStats(document.getElementById('stats-container'), s, r);
				renderDnsServerStatus(document.getElementById('dns-server-container'), st);
			});
		}, 5000);

		return m;
	},

	handleSaveApply: function(ev) {
		var newEnabled = document.getElementById('cb-enabled').checked;
		apiPost('/service', { enabled: newEnabled }).then(function() {
			uci.set('lynxdns', 'main', 'enabled', newEnabled ? '1' : '0');
			uci.save();
			uci.apply();
			showToast('设置已保存成功。');
		});
	},

	handleSave: function(ev) {
		return this.handleSaveApply(ev);
	}
});
