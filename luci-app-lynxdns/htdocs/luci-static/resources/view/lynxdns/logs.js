'use strict';
'require view';
'require ui';

var apiBase;
var logWs = null;
var queryWs = null;
var wsConnected = false;
var logAutoFollow = true;
var activeCatFilter = '';

var catDefs = {
	'系统':    { 'bg': '#2a1a4a', 'color': '#b388ff' },
	'远程DNS': { 'bg': '#1a2a4a', 'color': '#64b5f6' },
	'国内DNS': { 'bg': '#1a3a1a', 'color': '#81c784' },
	'DNS':     { 'bg': '#1a2a3a', 'color': '#80cbc4' },
	'Geo':     { 'bg': '#1a3a3a', 'color': '#4dd0e1' },
	'广告':    { 'bg': '#3a0a2a', 'color': '#f48fb1' },
	'服务':    { 'bg': '#1a2a3a', 'color': '#80cbc4' },
	'配置':    { 'bg': '#3a2a0a', 'color': '#ffe082' },
	'Web':     { 'bg': '#2a2a3a', 'color': '#b0bec5' }
};

function apiCall(method, path, data) {
	return new Promise(function(resolve) {
		var xhr = new XMLHttpRequest();
		xhr.open(method, (apiBase || L.url('admin/services/lynxdns/api')) + path, true);
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

function apiGet(p) { return apiCall('GET', p, null); }
function apiPost(p, d) { return apiCall('POST', p, d); }
function apiDelete(p) { return apiCall('DELETE', p, null); }

function showToast(msg) {
	var existing = document.querySelectorAll('.lynxdns-toast');
	existing.forEach(function(el) { el.remove(); });
	var t = E('div', { 'class': 'lynxdns-toast' }, msg);
	document.body.appendChild(t);
	setTimeout(function() { if (t.parentNode) t.remove(); }, 2500);
}

function fieldRow(label, content) {
	return E('div', { 'class': 'cbi-value' }, [
		E('label', { 'class': 'cbi-value-title' }, label),
		E('div', { 'class': 'cbi-value-field' },
			(typeof content === 'string') ? [E('span', {}, content)] : [content])
	]);
}

function getTimestamp() {
	var d = new Date();
	return d.getHours().toString().padStart(2, '0') + ':' +
		d.getMinutes().toString().padStart(2, '0') + ':' +
		d.getSeconds().toString().padStart(2, '0') + '.' +
		d.getMilliseconds().toString().padStart(3, '0');
}

function utcToLocal(utcStr) {
	if (!utcStr) return '';
	var d = new Date(utcStr);
	if (isNaN(d.getTime())) {
		var t = utcStr.match(/T(\d{2}:\d{2}:\d{2})/);
		return t ? t[1] : utcStr;
	}
	return d.getHours().toString().padStart(2, '0') + ':' +
		d.getMinutes().toString().padStart(2, '0') + ':' +
		d.getSeconds().toString().padStart(2, '0');
}

function getLogCategory(message) {
	var ml = message.toLowerCase();
	if (ml.indexOf('started') >= 0 || ml.indexOf('stopped') >= 0 || ml.indexOf('shutting down') >= 0 ||
		ml.indexOf('signal') >= 0 || ml.indexOf('lynxdns') >= 0 && (ml.indexOf('success') >= 0 || ml.indexOf('init') >= 0)) {
		return '系统';
	}
	if (ml.indexOf('upstream dns') >= 0) {
		if (ml.indexOf('8.8.8.8') >= 0 || ml.indexOf('1.1.1.1') >= 0 || ml.indexOf('tls://') >= 0 || ml.indexOf('doh') >= 0 || ml.indexOf('https://') >= 0) {
			return '远程DNS';
		}
		if (ml.indexOf('119.29.29.29') >= 0 || ml.indexOf('223.5.5.5') >= 0 || ml.indexOf('114.114') >= 0 || ml.indexOf('udp://') >= 0) {
			return '国内DNS';
		}
		return 'DNS';
	}
	if (ml.indexOf('geoip') >= 0 || ml.indexOf('geosite') >= 0) return 'Geo';
	if (ml.indexOf('ad_filter') >= 0 || ml.indexOf('ad filter') >= 0 || ml.indexOf('adblock') >= 0) return '广告';
	if (ml.indexOf('api') >= 0 || ml.indexOf('server starting') >= 0 || ml.indexOf('listening') >= 0) return '服务';
	if (ml.indexOf('rule') >= 0 || ml.indexOf('config') >= 0 || ml.indexOf('reload') >= 0) return '配置';
	if (ml.indexOf('luci') >= 0) return 'Web';
	return '';
}

function catBadge(name) {
	var c = catDefs[name];
	if (!c) return '';
	return '<span style="display:inline-block;padding:0 4px;border-radius:3px;font-size:9px;font-weight:bold;background:' + c.bg + ';color:' + c.color + '">' + name + '</span> ';
}

function parseLogMessage(line) {
	var cleaned = line.replace(/^\w{3}\s+\w{3}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}\s+\d{4}\s+\S+\s+\S+\[\d+\]:\s*/, '');
	var level = '';
	var timestamp = '';
	var message = cleaned;
	var isoMatch = cleaned.match(/\[(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[\d.]*Z)\]\s*/);
	var levelMatch = cleaned.match(/\b(DEBUG|INFO|WARN|WARNING|ERROR)\b/i);
	if (levelMatch) {
		level = levelMatch[1].toLowerCase();
	}
	if (isoMatch) {
		var iso = isoMatch[1];
		timestamp = utcToLocal(iso);
		var afterIso = cleaned.substring(isoMatch.index + isoMatch[0].length);
		var afterLevel = afterIso.replace(/^(DEBUG|INFO|WARN|WARNING|ERROR)\s*/i, '');
		message = afterLevel.trim();
	} else if (levelMatch) {
		var beforeLevel = cleaned.substring(0, levelMatch.index).trim();
		var afterLevel2 = cleaned.substring(levelMatch.index + levelMatch[0].length).trim();
		var tMatch = beforeLevel.match(/(\d{2}:\d{2}:\d{2})/);
		if (tMatch) timestamp = tMatch[1];
		message = afterLevel2;
	}
	return { level: level, timestamp: timestamp, message: message };
}

function colorizeLogLine(line) {
	if (!line || !line.trim()) return '';
	var levelColors = {
		'debug': { 'bg': '#3a3a4a', 'text': '#999' },
		'info': { 'bg': '#1a3a5c', 'text': '#4a90d9' },
		'warn': { 'bg': '#3a2a0a', 'text': '#f0ad4e' },
		'warning': { 'bg': '#3a2a0a', 'text': '#f0ad4e' },
		'error': { 'bg': '#3a1a1a', 'text': '#d9534f' }
	};
	var p = parseLogMessage(line);
	var escaped = p.message.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
	var lc = levelColors[p.level] || { 'bg': '#2a2a3e', 'text': '#ccc' };
	var badge = '';
	if (p.level) {
		var label = p.level.toUpperCase();
		badge = '<span style="display:inline-block;padding:0 5px;border-radius:3px;' +
			'font-size:10px;font-weight:bold;background:' + lc.bg + ';color:' + lc.text + '">' +
			label + '</span> ';
	}
	var catName = getLogCategory(p.message);
	var cat = catBadge(catName);
	var timeHtml = p.timestamp
		? '<span style="color:#666;font-size:11px;margin-right:4px">' + p.timestamp + '</span>'
		: '';
	return '<div data-cat="' + catName + '" style="padding:2px 4px;border-bottom:1px solid #222;word-break:break-all;white-space:pre-wrap;line-height:1.6">' +
		timeHtml + badge + cat + '<span style="color:' + lc.text + '">' + escaped + '</span></div>';
}

function applyCatFilter() {
	var el = document.getElementById('log-content');
	if (!el) return;
	var children = el.children;
	for (var i = 0; i < children.length; i++) {
		var cat = children[i].getAttribute('data-cat') || '';
		children[i].style.display = (!activeCatFilter || cat === activeCatFilter) ? '' : 'none';
	}
}

function renderLogHtml(text) {
	if (!text) return '<div style="color:#666;padding:8px">暂无日志，等待数据...</div>';
	var lines = text.split('\n');
	var html = '';
	for (var i = lines.length - 1; i >= 0; i--) {
		html += colorizeLogLine(lines[i]);
	}
	return html || '<div style="color:#666;padding:8px">暂无日志，等待数据...</div>';
}

function colorizeQueryLine(q) {
	var actionColors = {
		domestic: { 'bg': '#1a3a1a', 'text': '#5cb85c', 'label': '国内' },
		remote: { 'bg': '#1a2a4a', 'text': '#4a90d9', 'label': '远程' },
		blocked: { 'bg': '#3a1a1a', 'text': '#d9534f', 'label': '拦截' },
		ad_block: { 'bg': '#3a0a2a', 'text': '#e74c9c', 'label': '广告拦截' },
		leak_blocked: { 'bg': '#3a1a0a', 'text': '#e67e22', 'label': '防泄漏' },
		rule_blocked: { 'bg': '#3a1a1a', 'text': '#d9534f', 'label': '规则拦截' },
		redirected: { 'bg': '#2a1a3a', 'text': '#9b59b6', 'label': '重定向' },
		cache_hit: { 'bg': '#3a2a0a', 'text': '#f0ad4e', 'label': '缓存' },
		'default': { 'bg': '#2a2a3e', 'text': '#aaa', 'label': '默认' }
	};
	var action = q.action || 'default';
	var rule = q.matched_rule || '';
	if (action === 'blocked') {
		if (rule.indexOf('ad_filter') >= 0) {
			action = 'ad_block';
		} else if (rule.indexOf('leak_protection') >= 0) {
			action = 'leak_blocked';
		} else {
			action = 'rule_blocked';
		}
	}
	var ac = actionColors[action] || actionColors['default'];
	var time = q.timestamp ? utcToLocal(q.timestamp) : getTimestamp();
	var domain = (q.domain || '?').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
	var ips = (q.ips || []).join(', ');
	var server = q.server_used || '';
	var latency = (q.latency_ms || 0).toFixed(1);
	var cached = q.cached;
	var type = q.type || '';
	var badge = '<span style="display:inline-block;padding:0 6px;border-radius:3px;' +
		'font-size:10px;font-weight:bold;background:' + ac.bg + ';color:' + ac.text + '">' +
		ac.label + '</span> ';
	var domainHtml = '<span style="color:#fff;font-weight:bold">' + domain + '</span>';
	var typeHtml = type ? '<span style="color:#888;font-size:10px">(' + type + ')</span>' : '';
	var ipHtml = ips ? '<span style="color:#5cb85c"> → ' + ips + '</span>' : '';
	var serverHtml = server
		? '<span style="color:#666;font-size:11px"> [' + server + ' ' + latency + 'ms]</span>'
		: '';
	var cachedHtml = cached ? '<span style="color:#f0ad4e;font-size:10px"> (缓存)</span>' : '';
	var ruleHtml = '';
	if (rule && action !== 'default') {
		var ruleLabel = rule;
		if (rule === 'ad_filter') ruleLabel = '广告过滤';
		else if (rule === 'geosite:ad_filter') ruleLabel = 'Geosite广告';
		else if (rule.indexOf('leak_protection') >= 0) ruleLabel = '泄漏保护';
		ruleHtml = '<span style="color:#555;font-size:10px;margin-left:4px">[' + ruleLabel + ']</span>';
	}
	return '<div style="padding:3px 4px;border-bottom:1px solid #222;word-break:break-all;white-space:pre-wrap;line-height:1.6;background:#1e1e32">' +
		'<span style="color:#666;font-size:11px;margin-right:4px">' + time + '</span>' +
		badge + domainHtml + ' ' + typeHtml + ipHtml + cachedHtml + serverHtml + ruleHtml +
		'</div>';
}

return view.extend({
	load: function() {
		apiBase = L.url('admin/services/lynxdns/api');
		return Promise.all([
			apiGet('/log_read?lines=200').catch(function() { return { code: 500 }; }),
			apiGet('/ws_config').catch(function() { return { code: 500 }; })
		]);
	},

	handleSaveApply: null,
	handleSave: null,
	handleReset: null,

	render: function(data) {
		if (!document.querySelector('link[href*="lynxdns.css"]')) {
			document.head.appendChild(E('link', { 'rel': 'stylesheet', 'type': 'text/css', 'href': '/luci-static/lynxdns.css' }));
		}
		var logResp = data[0] || {};
		var wsConfigResp = data[1] || {};

		var logData = (logResp.code === 0) ? logResp.data : null;
		var wsConfig = (wsConfigResp.code === 0) ? wsConfigResp.data : null;

		var m = E('div', { 'class': 'cbi-map' }, [
			E('div', { 'class': 'cbi-map-descr' }, '实时运行日志和诊断工具。')
		]);

		var toolsSection = E('fieldset', { 'class': 'cbi-section', 'style': 'padding:20px' }, [
			E('legend', {}, '工具')
		]);

		var lookupInput = E('input', {
			'type': 'text',
			'class': 'cbi-input-text',
			'placeholder': '例如 www.google.com',
			'style': 'flex:1;min-width:120px;height:40px;box-sizing:border-box'
		});
		var lookupTypeSelect = E('select', {
			'class': 'cbi-input-select',
			'style': 'height:40px;box-sizing:border-box'
		}, [
			E('option', { 'value': 'A', 'selected': true }, 'A'),
			E('option', { 'value': 'AAAA' }, 'AAAA'),
			E('option', { 'value': 'MX' }, 'MX'),
			E('option', { 'value': 'CNAME' }, 'CNAME'),
			E('option', { 'value': 'TXT' }, 'TXT')
		]);
		var lookupBtn = E('button', {
			'class': 'cbi-button cbi-button-apply',
			'type': 'button'
		}, '查询');
		var lookupResult = E('div', {
			'id': 'lookup-result',
			'style': 'margin-top:8px;width:100%'
		});

		lookupBtn.addEventListener('click', function() {
			var domain = lookupInput.value.trim();
			if (!domain) return;
			lookupResult.innerHTML = '<em>正在解析...</em>';
			apiPost('/dns_lookup', {
				domain: domain,
				type: lookupTypeSelect.value
			}).then(function(resp) {
				if (resp.code === 0 && resp.data) {
					var r = resp.data;
					lookupResult.innerHTML =
						'<div style="padding:8px;background:#f5f5f5;border-radius:4px">' +
						'<strong>' + r.domain + '</strong> (' + r.type + ') --> ' +
						(r.ips || []).join(', ') +
						'<br>TTL: ' + r.ttl + ' 秒 | 服务器: ' + r.server_used +
						' | 延迟: ' + r.latency_ms + ' ms' +
						'<br>动作: ' + r.action +
						' | 匹配规则: ' + (r.matched_rule || '无') +
						'</div>';
				} else {
					lookupResult.innerHTML =
						'<span style="color:red">查询失败: ' +
						(resp.message || '未知错误') + '</span>';
				}
			});
		});

		toolsSection.appendChild(E('div', { 'class': 'cbi-value', 'style': 'padding:0 2px;min-height:48px' }, [
			E('div', {
				'class': 'cbi-value-field',
				'style': 'display:flex;align-items:flex-start;flex-wrap:wrap;gap:8px;width:100%'
			}, [lookupInput, lookupTypeSelect, lookupBtn, lookupResult])
		]));
		m.appendChild(toolsSection);

		var logSection = E('fieldset', { 'class': 'cbi-section', 'style': 'padding:20px' }, [
			E('legend', {}, '运行日志')
		]);

		var logControls = E('div', {
			'style': 'display:flex;gap:8px;margin-bottom:8px;align-items:center;flex-wrap:wrap'
		});

		var levelSelect = E('select', {
			'class': 'cbi-input-select',
			'style': 'height:40px'
		}, [
			E('option', { 'value': '', 'selected': true }, '全部级别'),
			E('option', { 'value': 'debug' }, 'Debug (调试)'),
			E('option', { 'value': 'info' }, 'Info (信息)'),
			E('option', { 'value': 'warn' }, 'Warning (警告)'),
			E('option', { 'value': 'error' }, 'Error (错误)')
		]);
		logControls.appendChild(E('label', {
			'style': 'display:flex;align-items:center;gap:4px'
		}, ['级别: ', levelSelect]));

		var catOptions = [E('option', { 'value': '', 'selected': true }, '全部分类')];
		Object.keys(catDefs).forEach(function(key) {
			catOptions.push(E('option', { 'value': key }, key));
		});
		var catSelect = E('select', {
			'class': 'cbi-input-select',
			'style': 'height:40px'
		}, catOptions);
		logControls.appendChild(E('label', {
			'style': 'display:flex;align-items:center;gap:4px'
		}, ['分类: ', catSelect]));

		catSelect.addEventListener('change', function() {
			activeCatFilter = this.value;
			applyCatFilter();
		});

		var clearLogBtn = E('button', {
			'class': 'cbi-button cbi-button-remove',
			'type': 'button'
		}, '清空日志');
		clearLogBtn.addEventListener('click', function() {
			if (confirm('确定要清空所有日志吗？')) {
				apiDelete('/log_clear').then(function(resp) {
					var el = document.getElementById('log-content');
					if (el) el.innerHTML = '';
					showToast(resp.code === 0 ? '日志已清空。' : '清空失败: ' + (resp.message || ''));
				});
			}
		});
		logControls.appendChild(clearLogBtn);

		logSection.appendChild(logControls);

		var logContainer = E('div', {
			'id': 'log-content',
			'style': 'width:100%;height:400px;font-family:monospace;' +
				'font-size:12px;resize:vertical;overflow-y:auto;overflow-x:hidden;' +
				'background:#1a1a2e;color:#e0e0e0;' +
				'border:1px solid #333;padding:8px'
		});
		logContainer.innerHTML = renderLogHtml((logData && logData.logs) ? logData.logs : '');
		logSection.appendChild(logContainer);
		logContainer.addEventListener('scroll', function() {
			logAutoFollow = (this.scrollTop < 10);
		});
		m.appendChild(logSection);

		startAutoRefresh();

		function initWebSocket(config, levelSelect) {
			var wsHost = config.host || window.location.hostname;
			var wsPort = config.port || '5335';
			var wsToken = config.secret || '';
			var wsProtocol = (window.location.protocol === 'https:') ? 'wss:' : 'ws:';

			function connectLogWs() {
				var level = levelSelect ? levelSelect.value : '';
				var url = wsProtocol + '//' + wsHost + ':' + wsPort +
					'/api/v1/stream/logs?token=' + encodeURIComponent(wsToken);
				if (level) url += '&level=' + level;

				try {
					logWs = new WebSocket(url);

					logWs.onopen = function() {
						wsConnected = true;
					};

					logWs.onmessage = function(event) {
						var el = document.getElementById('log-content');
						if (!el) return;
						var line = '';
						try {
							var entry = JSON.parse(event.data);
							line = '[' + (entry.timestamp || '') + '] ' +
								'[' + (entry.level || '') + '] ' +
								(entry.message || event.data);
						} catch (e) {
							line = event.data;
						}
						var html = colorizeLogLine(line);
						if (activeCatFilter) {
							var p = parseLogMessage(line);
							var catName = getLogCategory(p.message);
							if (catName !== activeCatFilter) return;
						}
						el.innerHTML = html + el.innerHTML;
						while (el.children.length > 500) {
							el.removeChild(el.lastChild);
						}
						if (logAutoFollow) {
							el.scrollTop = 0;
						}
					};

					logWs.onerror = function() {
						wsConnected = false;
					};

					logWs.onclose = function() {
						wsConnected = false;
						setTimeout(connectLogWs, 5000);
					};
				} catch (e) {
					wsConnected = false;
				}
			}

			connectLogWs();

			function connectQueryWs() {
				var url = wsProtocol + '//' + wsHost + ':' + wsPort +
					'/api/v1/stream/queries?token=' + encodeURIComponent(wsToken);
				try {
					queryWs = new WebSocket(url);
					queryWs.onmessage = function(event) {
						var el = document.getElementById('log-content');
						if (!el) return;
						try {
							var q = JSON.parse(event.data);
							el.innerHTML = colorizeQueryLine(q) + el.innerHTML;
							while (el.children.length > 500) {
								el.removeChild(el.lastChild);
							}
							if (logAutoFollow) {
								el.scrollTop = 0;
							}
						} catch (e) { /* ignore */ }
					};
					queryWs.onerror = function() {
						wsConnected = false;
					};
					queryWs.onclose = function() {
						setTimeout(connectQueryWs, 5000);
					};
				} catch (e) { /* ignore */ }
			}
			connectQueryWs();

			if (levelSelect) {
				levelSelect.addEventListener('change', function() {
					if (logWs) {
						logWs.close();
						logWs = null;
					}
					connectLogWs();
				});
			}
		}

		var lastLogContent = '';

		function refreshLogs() {
			apiGet('/log_read?lines=200').then(function(resp) {
				if (resp.code !== 0 || !resp.data || !resp.data.logs) return;
				var el = document.getElementById('log-content');
				if (!el) return;
				var logs = resp.data.logs;
				if (logs === lastLogContent) return;
				lastLogContent = logs;
				el.innerHTML = renderLogHtml(logs);
				if (activeCatFilter) applyCatFilter();
				if (logAutoFollow) {
					el.scrollTop = 0;
				}
			});
		}

		var logTimer = null;

		function startAutoRefresh() {
			stopAutoRefresh();
			logTimer = setInterval(refreshLogs, 500);
			if (wsConfig) {
				initWebSocket(wsConfig, levelSelect);
			}
		}

		function stopAutoRefresh() {
			if (logTimer) { clearInterval(logTimer); logTimer = null; }
			if (logWs) { try { logWs.close(); } catch (e) { /* ignore */ } logWs = null; }
			if (queryWs) { try { queryWs.close(); } catch (e) { /* ignore */ } queryWs = null; }
			wsConnected = false;
		}

		return m;
	}
});
