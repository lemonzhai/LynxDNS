'use strict';
'require view';
'require ui';

var apiBase;
var logWs = null;
var queryWs = null;
var wsConnected = false;
var logAutoFollow = true;
var activeCatFilter = '';

var queryCatDefs = {
	'remote':    { 'bg': '#1a2233', 'color': '#81CFE0', 'label': '远程' },
	'domestic':  { 'bg': '#1a2a1d', 'color': '#33C192', 'label': '国内' },
	'cache_hit': { 'bg': '#2a2518', 'color': '#F0A020', 'label': '缓存' },
	'ad_block':  { 'bg': '#2a1a1e', 'color': '#F65A5A', 'label': '广告拦截' },
	'leak_block':{ 'bg': '#2a2218', 'color': '#F29D79', 'label': '防泄漏' },
	'rule_block':{ 'bg': '#2a1a1e', 'color': '#F65A5A', 'label': '规则拦截' },
	'redirected':{ 'bg': '#231a2e', 'color': '#B38CFF', 'label': '重定向' },
	'failed':    { 'bg': '#2a1a1e', 'color': '#F65A5A', 'label': '超时' },
	'default':   { 'bg': '#222427', 'color': '#9599A6', 'label': '默认' }
};

var sysCatDefs = {
	'sys': { 'bg': '#2a1a4a', 'color': '#b388ff' }
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

function getSysCategory(message) {
	if (/^(国内|远程|缓存|广告拦截|防泄漏|规则拦截|重定向|超时|默认)\s/.test(message)) return '';
	return 'sys';
}

function catBadge(name, defs) {
	var c = defs[name];
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

function latencyColor(ms) {
	if (ms < 50) return '#33C192';
	if (ms < 200) return '#F0A020';
	return '#F65A5A';
}

function formatIpsHtml(ips) {
	if (!ips || ips.length === 0) return '<span style="color:#666"> → (无记录)</span>';
	var cnames = [];
	var addrs = [];
	ips.forEach(function(ip) {
		if (ip.indexOf(':') >= 0 && ip.indexOf('.') < 0 && !ip.match(/^\[?[0-9a-f:]+\]?$/i)) {
			cnames.push(ip);
		} else if (ip.match(/^[\d.:a-f]+$/i)) {
			addrs.push(ip);
		} else {
			cnames.push(ip);
		}
	});
	var parts = [];
	if (cnames.length > 0) {
		parts.push('<span style="color:#B38CFF">CNAME:' + cnames.join(',').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;') + '</span>');
	}
	if (addrs.length > 0) {
		var displayAddrs;
		if (addrs.length > 3) {
			displayAddrs = addrs.slice(0, 3);
			var extra = addrs.length - 3;
			parts.push('<span style="color:#33C192">' + displayAddrs.join(', ') + '</span>' +
				'<span class="lynxdns-ip-expand" data-ips="' + addrs.join(',').replace(/"/g, '&quot;') + '" ' +
				'style="color:#737780;cursor:pointer;font-size:10px;margin-left:2px">+' + extra + '</span>');
		} else {
			parts.push('<span style="color:#33C192">' + addrs.join(', ') + '</span>');
		}
	}
	return ' → ' + parts.join(' <span style="color:#737780">|</span> ');
}

var actionLabelMap = {
	'国内': 'domestic', '远程': 'remote', '缓存': 'cache_hit',
	'广告拦截': 'ad_block', '防泄漏': 'leak_block', '规则拦截': 'rule_block',
	'重定向': 'redirected', '超时': 'failed', '默认': 'default'
};

function parseQueryFromText(msg) {
	var m = msg.match(/^(国内|远程|缓存|广告拦截|防泄漏|规则拦截|重定向|超时|默认)\s+(.+)$/);
	if (!m) return null;
	var action = actionLabelMap[m[1]] || 'default';
	var rest = m[2];
	var q = { action: action, domain: '', ips: [], server_used: '', latency_ms: 0, matched_rule: '' };

	if (action === 'cache_hit') {
		q.domain = rest.trim();
		return q;
	}

	if (action === 'failed') {
		var failMatch = rest.match(/^(\S+)\s+(\d+)ms\s+(\S+)\s+(.+)$/);
		if (failMatch) {
			q.domain = failMatch[1];
			q.latency_ms = parseFloat(failMatch[2]);
			q.server_used = failMatch[3];
			q.matched_rule = failMatch[4];
		} else {
			q.domain = rest.trim();
		}
		return q;
	}

	if (action === 'ad_block' || action === 'leak_block' || action === 'rule_block') {
		var blockMatch = rest.match(/^(\S+)\s+\[(.+)\]$/);
		if (blockMatch) {
			q.domain = blockMatch[1];
			q.matched_rule = blockMatch[2];
		} else {
			q.domain = rest.trim();
		}
		return q;
	}

	var arrowIdx = rest.indexOf('→');
	if (arrowIdx >= 0) {
		q.domain = rest.substring(0, arrowIdx).trim();
		var after = rest.substring(arrowIdx + 1);
		var ruleMatch = after.match(/\[([^\]]+)\]\s*$/);
		if (ruleMatch) {
			q.matched_rule = ruleMatch[1];
			after = after.substring(0, ruleMatch.index).trim();
		}
		var serverMatch = after.match(/(\d+)ms\s+(\S+)\s*$/);
		if (serverMatch) {
			q.latency_ms = parseFloat(serverMatch[1]);
			q.server_used = serverMatch[2];
			after = after.substring(0, serverMatch.index).trim();
		}
		var ipPart = after.trim();
		if (ipPart === '(无记录)') {
			q.ips = [];
		} else {
			var sections = ipPart.split(/\s*\|\s*/);
			sections.forEach(function(sec) {
				if (sec.indexOf('CNAME:') === 0) {
					q.ips.push(sec.substring(7));
				} else if (sec) {
					q.ips.push(sec);
				}
			});
		}
	} else {
		q.domain = rest.trim();
	}
	return q;
}

function colorizeLogLine(line) {
	if (!line || !line.trim()) return '';
	var p = parseLogMessage(line);
	var q = parseQueryFromText(p.message);
	if (q) {
		q.timestamp = p.timestamp ? p.timestamp : '';
		return colorizeQueryLine(q);
	}
	var levelColors = {
		'debug': { 'bg': '#3a3a4a', 'text': '#999' },
		'info': { 'bg': '#1a3a5c', 'text': '#4a90d9' },
		'warn': { 'bg': '#3a2a0a', 'text': '#f0ad4e' },
		'warning': { 'bg': '#3a2a0a', 'text': '#f0ad4e' },
		'error': { 'bg': '#3a1a1a', 'text': '#d9534f' }
	};
	var escaped = p.message.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
	var lc = levelColors[p.level] || { 'bg': '#2a2a3e', 'text': '#ccc' };
	var badge = '';
	if (p.level) {
		var label = p.level.toUpperCase();
		badge = '<span style="display:inline-block;padding:0 5px;border-radius:3px;' +
			'font-size:10px;font-weight:bold;background:' + lc.bg + ';color:' + lc.text + '">' +
			label + '</span> ';
	}
	var catName = getSysCategory(p.message);
	var timeHtml = p.timestamp
		? '<span style="color:#666;font-size:11px;margin-right:4px">' + p.timestamp + '</span>'
		: '';
	return '<div data-type="sys" data-cat="' + catName + '" style="padding:2px 4px;border-bottom:1px solid #222;word-break:break-all;white-space:pre-wrap;line-height:1.6">' +
		timeHtml + badge + '<span style="color:' + lc.text + '">' + escaped + '</span></div>';
}

function applyCatFilter() {
	var el = document.getElementById('log-content');
	if (!el) return;
	var children = el.children;
	for (var i = 0; i < children.length; i++) {
		var type = children[i].getAttribute('data-type') || '';
		var cat = children[i].getAttribute('data-cat') || '';
		if (!activeCatFilter) {
			children[i].style.display = '';
		} else if (activeCatFilter === 'sys') {
			children[i].style.display = (type === 'sys') ? '' : 'none';
		} else {
			children[i].style.display = (cat === activeCatFilter) ? '' : 'none';
		}
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
	var action = q.action || 'default';
	var ac = queryCatDefs[action] || queryCatDefs['default'];
	var time = q.timestamp ? utcToLocal(q.timestamp) : getTimestamp();
	var domain = (q.domain || '?').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
	var server = q.server_used || '';
	var latency = q.latency_ms || 0;
	var rule = q.matched_rule || '';

	var badge = '<span style="display:inline-block;padding:0 6px;border-radius:3px;' +
		'font-size:10px;font-weight:bold;background:' + ac.bg + ';color:' + ac.color + '">' +
		ac.label + '</span> ';
	var domainHtml = '<span style="color:#F29D79;font-weight:bold">' + domain + '</span>';

	var resultHtml = '';
	var serverHtml = '';
	var ruleHtml = '';
	var bgStyle = 'background:' + ac.bg;

	if (action === 'cache_hit') {
	} else if (action === 'failed') {
		resultHtml = '<span style="color:#F65A5A"> ' + (rule || '查询失败') + '</span>';
		serverHtml = '<span style="color:#737780;font-size:11px"> ' + server + '</span>';
	} else if (action === 'ad_block' || action === 'leak_block' || action === 'rule_block') {
		var ruleLabel = rule;
		if (rule === 'ad_filter') ruleLabel = '广告过滤';
		else if (rule === 'geosite:ad_filter') ruleLabel = 'Geosite广告';
		else if (rule.indexOf('leak_protection') >= 0) ruleLabel = '泄漏保护';
		ruleHtml = ruleLabel ? '<span style="color:#737780;font-size:10px;margin-left:4px">[' + ruleLabel + ']</span>' : '';
	} else if (action === 'redirected') {
		resultHtml = formatIpsHtml(q.ips);
		serverHtml = '<span style="color:#737780;font-size:11px"> ' + server + '</span>';
		ruleHtml = rule ? '<span style="color:#737780;font-size:10px;margin-left:4px">[' + rule + ']</span>' : '';
	} else {
		resultHtml = formatIpsHtml(q.ips);
		var latColor = latencyColor(latency);
		serverHtml = server
			? '<span style="color:#737780;font-size:11px"> ' + server + ' <span style="color:' + latColor + '">' + latency.toFixed(0) + 'ms</span></span>'
			: '';
		if (rule && rule !== 'geosite:matched' && rule !== 'default_policy') {
			ruleHtml = '<span style="color:#737780;font-size:10px;margin-left:4px">[' + rule + ']</span>';
		}
	}

	return '<div data-type="query" data-cat="' + action + '" style="padding:3px 4px;border-bottom:1px solid #222;word-break:break-all;white-space:pre-wrap;line-height:1.6;' + bgStyle + '">' +
		'<span style="color:#666;font-size:11px;margin-right:4px">' + time + '</span>' +
		badge + domainHtml + resultHtml + serverHtml + ruleHtml +
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

		var catOptions = [E('option', { 'value': '', 'selected': true }, '全部')];
		Object.keys(queryCatDefs).forEach(function(key) {
			catOptions.push(E('option', { 'value': key }, queryCatDefs[key].label));
		});
		catOptions.push(E('option', { 'value': 'sys' }, '系统日志'));

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
				'font-size:13px;resize:vertical;overflow-y:auto;overflow-x:hidden;' +
				'background:#1A1B1D;color:#D1D3DB;' +
				'border:1px solid #2A2D31;padding:8px'
		});
		logContainer.innerHTML = renderLogHtml((logData && logData.logs) ? logData.logs : '');
		logSection.appendChild(logContainer);
		logContainer.addEventListener('scroll', function() {
			logAutoFollow = (this.scrollTop < 10);
		});

		logContainer.addEventListener('click', function(e) {
			var target = e.target;
			if (target && target.classList && target.classList.contains('lynxdns-ip-expand')) {
				var ips = target.getAttribute('data-ips');
				if (ips) {
					var span = document.createElement('span');
					span.style.cssText = 'color:#5cb85c';
					span.textContent = ', ' + ips.split(',').slice(3).join(', ');
					target.parentNode.replaceChild(span, target);
				}
			}
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
							var catName = getSysCategory(p.message);
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
							var html = colorizeQueryLine(q);
							if (activeCatFilter) {
								var action = q.action || 'default';
								if (action !== activeCatFilter) return;
							}
							el.innerHTML = html + el.innerHTML;
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
