'use strict';
'require view';
'require ui';

var apiBase;
var domesticList, remoteList, defaultSelect, bootstrapList;

function showToast(msg) {
	var existing = document.querySelectorAll('.lynxdns-toast');
	existing.forEach(function(el) { el.remove(); });
	var t = E('div', { 'class': 'lynxdns-toast' }, msg);
	document.body.appendChild(t);
	setTimeout(function() { if (t.parentNode) t.remove(); }, 2500);
}

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

function apiGet(p) { return apiCall('GET', p, null); }
function apiPatch(p, d) { return apiCall('PATCH', p, d); }

function fieldRow(label, content) {
	return E('div', { 'class': 'cbi-value' }, [
		E('label', { 'class': 'cbi-value-title' }, label),
		E('div', { 'class': 'cbi-value-field' }, (typeof content === 'string') ? [E('span', {}, content)] : [content])
	]);
}

function createDnsList(items, placeholder) {
	var container = E('div', { 'class': 'dns-list-container', 'style': 'margin-bottom:5px' });
	var list = items || [];

	function renderItem(addr) {
		var row = E('div', { 'style': 'display:flex;margin-bottom:4px;gap:4px' }, [
			E('input', {
				'type': 'text', 'class': 'cbi-input-text',
				'value': addr, 'placeholder': placeholder
			}),
			E('button', {
				'class': 'cbi-button cbi-button-remove',
				'type': 'button', 'style': 'min-width:30px',
				'click': function() { row.parentNode.removeChild(row); }
			}, 'x')
		]);
		container.appendChild(row);
	}

	list.forEach(renderItem);

	var addBtn = E('button', {
		'class': 'cbi-button cbi-button-add',
		'type': 'button',
		'click': function() { renderItem(''); }
	}, '添加');
	container.appendChild(addBtn);

	container.getValues = function() {
		var inputs = container.querySelectorAll('input[type="text"]');
		var vals = [];
		inputs.forEach(function(inp) {
			if (inp.value.trim()) vals.push(inp.value.trim());
		});
		return vals;
	};

	return container;
}

return view.extend({
	load: function() {
		apiBase = L.url('admin/services/lynxdns/api');
		return apiGet('/config').catch(function() { return { code: 500 }; });
	},

	render: function(data) {
		if (!document.querySelector('link[href*="lynxdns.css"]')) {
			document.head.appendChild(E('link', { 'rel': 'stylesheet', 'type': 'text/css', 'href': '/luci-static/lynxdns.css' }));
		}
		var config = (data && data.code === 0) ? data.data : {};
		var dns = config.dns || {};

		var m = E('div', { 'class': 'cbi-map' }, [
			E('div', { 'class': 'cbi-map-descr' }, '配置国内外 DNS 上游服务器，支持 UDP、DoT、DoH 等多种协议。')
		]);

		domesticList = createDnsList(dns.domestic || ['udp://223.5.5.5:53', 'udp://119.29.29.29:53'], 'udp://223.5.5.5:53');
		var domesticSection = E('fieldset', { 'class': 'cbi-section' }, [
			E('legend', {}, '国内 DNS 服务器'),
			E('div', { 'class': 'cbi-section-descr' }, '用于解析国内域名的 DNS 服务器。推荐：阿里 DNS、腾讯 DNS、百度 DNS。')
		]);
		domesticSection.appendChild(E('div', { 'class': 'cbi-value' }, [
			E('label', { 'class': 'cbi-value-title' }, '服务器列表'),
			E('div', { 'class': 'cbi-value-field' }, [domesticList])
		]));
		m.appendChild(domesticSection);

		remoteList = createDnsList(dns.remote || ['tls://8.8.8.8:853'], '');
		var remoteSection = E('fieldset', { 'class': 'cbi-section' }, [
			E('legend', {}, '远程 DNS 服务器'),
			E('div', { 'class': 'cbi-section-descr' }, '用于解析海外域名的 DNS 服务器。推荐：Cloudflare DoT、Google DoH。')
		]);
		remoteSection.appendChild(E('div', { 'class': 'cbi-value' }, [
			E('label', { 'class': 'cbi-value-title' }, '服务器列表'),
			E('div', { 'class': 'cbi-value-field' }, [remoteList])
		]));
		m.appendChild(remoteSection);

		defaultSelect = E('select', { 'class': 'cbi-input-select' }, [
			E('option', { 'value': 'domestic' }, '国内 DNS'),
			E('option', { 'value': 'remote' }, '远程 DNS')
		]);
		defaultSelect.value = (dns.default === 'remote') ? 'remote' : 'domestic';
		var defaultSection = E('fieldset', { 'class': 'cbi-section' }, [
			E('legend', {}, '默认 DNS'),
			E('div', { 'class': 'cbi-section-descr' }, '未匹配任何规则的域名所使用的默认 DNS 服务器。推荐使用国内 DNS。')
		]);
		defaultSection.appendChild(fieldRow('默认 DNS 服务器', defaultSelect));
		m.appendChild(defaultSection);

		bootstrapList = createDnsList(dns.bootstrap || ['udp://223.5.5.5:53', 'udp://119.29.29.29:53'], 'udp://223.5.5.5:53');
		var bootstrapSection = E('fieldset', { 'class': 'cbi-section' }, [
			E('legend', {}, '引导 DNS'),
			E('div', { 'class': 'cbi-section-descr' }, '用于解析 DoH/DoT 服务器域名的引导 DNS。请使用普通的 UDP DNS 服务器。')
		]);
		bootstrapSection.appendChild(E('div', { 'class': 'cbi-value' }, [
			E('label', { 'class': 'cbi-value-title' }, '服务器列表'),
			E('div', { 'class': 'cbi-value-field' }, [bootstrapList])
		]));
		m.appendChild(bootstrapSection);

		return m;
	},

	handleSaveApply: function(ev) {
		var patchData = {
			dns: {
				domestic: domesticList.getValues(),
				remote: remoteList.getValues(),
				default: defaultSelect.value,
				bootstrap: bootstrapList.getValues()
			}
		};
		apiPatch('/config', patchData).then(function(resp) {
			if (resp.code === 0) {
				showToast('DNS 设置已保存并热加载完成。');
			} else {
				showToast('保存失败：' + (resp.message || '未知错误'));
			}
		});
	},

	handleSave: function(ev) {
		return this.handleSaveApply(ev);
	}
});
