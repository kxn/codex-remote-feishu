import type { FeishuAppSummary, FeishuManifest } from "../../lib/types";
import { CapabilityCard } from "../../components/Cards";

type CapabilityStage = "permissions" | "events" | "longConnection" | "menus" | "publish" | "done";

function currentCapabilityStage(activeApp: FeishuAppSummary | null): CapabilityStage {
  if (!activeApp?.wizard?.scopesExportedAt) return "permissions";
  if (!activeApp.wizard.eventsConfirmedAt) return "events";
  if (!activeApp.wizard.callbacksConfirmedAt) return "longConnection";
  if (!activeApp.wizard.menusConfirmedAt) return "menus";
  if (!activeApp.wizard.publishedAt) return "publish";
  return "done";
}

export function StepCapabilityCheck(props: {
  activeApp: FeishuAppSummary | null;
  manifest: FeishuManifest;
  scopesJSON: string;
  permissionsConfirmed: boolean;
  eventsConfirmed: boolean;
  longConnectionConfirmed: boolean;
  menusConfirmed: boolean;
  busyAction: string;
  onPermissionsConfirmedChange: (val: boolean) => void;
  onEventsConfirmedChange: (val: boolean) => void;
  onLongConnectionConfirmedChange: (val: boolean) => void;
  onMenusConfirmedChange: (val: boolean) => void;
  onCopyScopes: () => void;
  onConfirmPermissions: () => void;
  onConfirmEvents: () => void;
  onConfirmLongConnection: () => void;
  onConfirmMenus: () => void;
  onCheckPublish: () => void;
}) {
  const capabilityStage = currentCapabilityStage(props.activeApp);
  const basicReady = capabilityStage === "done";

  return (
    <div className="wizard-step-layout">
      
      {!basicReady && (
         <div className="notice-banner warn" style={{marginBottom: '1rem'}}>
            现在还不能开始正常使用，请将必须要修好的核心配置处理完成。
         </div>
      )}

      <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0,1fr) minmax(0,1fr)', gap: '1.5rem', marginTop: '1.5rem' }}>
        
        {/* Left Column: Must Fix Now */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
          <h3 style={{ fontSize: '1.2rem', marginBottom: '0.2rem', borderBottom: '2px solid var(--accent-light)', paddingBottom: '0.5rem', display: 'inline-block' }}>必须先修好</h3>
          <CapabilityCard 
             title="核心聊天与交互" 
             description="确保这台机器上的应用能正常收发消息并处理主要卡片交互。"
             status={basicReady ? "passed" : "pending"} 
             actionNode={
                basicReady 
                 ? <div style={{width:'32px',height:'32px',borderRadius:'16px',background:'var(--accent)',color:'white',display:'flex',justifyContent:'center',alignItems:'center'}}>✓</div> 
                 : null
             }
          />
          {!basicReady && (
            <div style={{ marginTop: '0.5rem', background: '#fff', borderRadius: '1.2rem', border: '1px solid var(--border)', padding: '1.2rem' }}>
               <h4 style={{ margin: '0 0 0.8rem' }}>当前需要您的操作</h4>
               {capabilityStage === "permissions" && (
                 <>
                   <p style={{color:'var(--text-soft)', marginBottom:'1rem', fontSize: '0.9rem'}}>前往应用管理后台的“批量导入/导出权限”，贴入以下配置。</p>
                   <textarea className="code-textarea" readOnly value={props.scopesJSON} style={{marginBottom:'0.8rem'}} />
                   <button className="secondary-button" type="button" onClick={props.onCopyScopes} disabled={props.busyAction !== ""}>
                     复制基础权限配置
                   </button>
                   <label className="checkbox-card" style={{ display: 'flex', gap: '0.8rem', alignItems: 'center', marginTop: '1rem' }}>
                     <input type="checkbox" checked={props.permissionsConfirmed} onChange={(e) => props.onPermissionsConfirmedChange(e.target.checked)} />
                     <div>
                       <strong>我已经完成基础权限导入</strong>
                     </div>
                   </label>
                   <button className="primary-button" type="button" onClick={props.onConfirmPermissions} disabled={props.busyAction !== ""} style={{ marginTop: '1rem' }}>
                     记录并继续
                   </button>
                 </>
               )}
               {capabilityStage === "events" && (
                 <>
                   <p style={{color:'var(--text-soft)', marginBottom:'1rem'}}>请将事件配置为主版本事件订阅，并确保下述事件已被勾选。</p>
                   <label className="checkbox-card" style={{ display: 'flex', gap: '0.8rem', alignItems: 'center', marginTop: '1rem' }}>
                     <input type="checkbox" checked={props.eventsConfirmed} onChange={(e) => props.onEventsConfirmedChange(e.target.checked)} />
                     <div>
                       <strong>我已经完成事件订阅</strong>
                     </div>
                   </label>
                   <button className="primary-button" type="button" onClick={props.onConfirmEvents} disabled={props.busyAction !== ""} style={{ marginTop: '1rem' }}>
                     记录并继续
                   </button>
                 </>
               )}
               {capabilityStage === "longConnection" && (
                 <>
                   <p style={{color:'var(--text-soft)', marginBottom:'1rem'}}>请在后台找到长连接配置并勾下必须卡片项。</p>
                   <label className="checkbox-card" style={{ display: 'flex', gap: '0.8rem', alignItems: 'center', marginTop: '1rem' }}>
                     <input type="checkbox" checked={props.longConnectionConfirmed} onChange={(e) => props.onLongConnectionConfirmedChange(e.target.checked)} />
                     <div><strong>我已经完成卡片回调配置</strong></div>
                   </label>
                   <button className="primary-button" type="button" onClick={props.onConfirmLongConnection} disabled={props.busyAction !== ""} style={{ marginTop: '1rem' }}>记录并继续</button>
                 </>
               )}
               {capabilityStage === "menus" && (
                 <>
                    <p style={{color:'var(--text-soft)', marginBottom:'1rem'}}>核对机器人快捷面板事件 Key 设置。</p>
                    <label className="checkbox-card" style={{ display: 'flex', gap: '0.8rem', alignItems: 'center', marginTop: '1rem' }}>
                     <input type="checkbox" checked={props.menusConfirmed} onChange={(e) => props.onMenusConfirmedChange(e.target.checked)} />
                     <div><strong>我已经完成飞书应用菜单配置</strong></div>
                   </label>
                   <button className="primary-button" type="button" onClick={props.onConfirmMenus} disabled={props.busyAction !== ""} style={{ marginTop: '1rem' }}>记录并继续</button>
                 </>
               )}
               {capabilityStage === "publish" && (
                 <>
                   <p style={{color:'var(--text-soft)', marginBottom:'1rem'}}>所有必需项已经配置！最后请进行一次版本发布并点击检查。</p>
                   <button className="primary-button" type="button" onClick={props.onCheckPublish} disabled={props.busyAction !== ""} style={{ marginTop: '1rem' }}>检查发布状态验证</button>
                 </>
               )}
            </div>
          )}
        </div>

        {/* Right Column: Can Defer Later */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
          <h3 style={{ fontSize: '1.2rem', marginBottom: '0.2rem', borderBottom: '2px solid var(--surface-muted)', paddingBottom: '0.5rem', display: 'inline-block' }}>可以稍后补齐</h3>
          <CapabilityCard 
             title="私信状态提醒" 
             description="开启此功能，当机器人向您私信时，能够在列表外获得气泡提示。" 
             status="deferred" 
          />
          <CapabilityCard 
             title="Markdown 预览" 
             description="将本地 Markdown 链接实时转换为可视化的卡片进行交互展示。" 
             status="deferred" 
          />
          <div style={{ color: 'var(--text-soft)', marginTop: '0.5rem', fontSize: '0.9rem' }}>
            右侧的高级功能不阻塞第一步配置。您随时可以在完成基本通讯后在 Admin 控制台中一键进行补齐。
          </div>
        </div>
      </div>
    </div>
  );
}
