#define SECURITY_WIN32
#include "broker.h"
#ifdef __MINGW32__
#include "credentialprovider_compat.h"
#else
#include <credentialprovider.h>
#endif
#include <climits>
#include <ntsecapi.h>
#include <wincred.h>
#include <atomic>
#include <new>
#include <cstring>

// {1D066A31-71D4-45F4-9128-37A36B1A4256}
static const GUID CLSID_Linad={0x1d066a31,0x71d4,0x45f4,{0x91,0x28,0x37,0xa3,0x6b,0x1a,0x42,0x56}};
static std::atomic<long> objects{0};
static HRESULT duplicate(const wchar_t* value,PWSTR* out){if(!out)return E_POINTER;*out=nullptr;size_t bytes=(wcslen(value)+1)*sizeof(wchar_t);auto p=static_cast<PWSTR>(CoTaskMemAlloc(bytes));if(!p)return E_OUTOFMEMORY;memcpy(p,value,bytes);*out=p;return S_OK;}
static HRESULT serialize(const linad::Grant& grant,CREDENTIAL_PROVIDER_USAGE_SCENARIO usage,CREDENTIAL_PROVIDER_CREDENTIAL_SERIALIZATION* out){
 wchar_t computer[MAX_COMPUTERNAME_LENGTH+1]{};DWORD size=MAX_COMPUTERNAME_LENGTH+1;if(!GetComputerNameW(computer,&size))return HRESULT_FROM_WIN32(GetLastError());
 LSA_HANDLE handle=nullptr;NTSTATUS status=LsaConnectUntrusted(&handle);if(status)return HRESULT_FROM_WIN32(LsaNtStatusToWinError(status));
 char negotiate[]="Negotiate";LSA_STRING package{static_cast<USHORT>(strlen(negotiate)),static_cast<USHORT>(sizeof(negotiate)),negotiate};ULONG auth=0;
 status=LsaLookupAuthenticationPackage(handle,&package,&auth);LsaDeregisterLogonProcess(handle);if(status)return HRESULT_FROM_WIN32(LsaNtStatusToWinError(status));
 const std::wstring domain=computer;
 const std::wstring* fields[3]{&domain,&grant.login,&grant.password};
 size_t bytes=sizeof(KERB_INTERACTIVE_UNLOCK_LOGON);for(const auto& field:fields)bytes+=field->size()*sizeof(wchar_t);
 auto buffer=static_cast<BYTE*>(CoTaskMemAlloc(bytes));if(!buffer)return E_OUTOFMEMORY;ZeroMemory(buffer,bytes);
 auto logon=reinterpret_cast<KERB_INTERACTIVE_UNLOCK_LOGON*>(buffer);
 logon->Logon.MessageType=usage==CPUS_UNLOCK_WORKSTATION?KerbWorkstationUnlockLogon:KerbInteractiveLogon;
 UNICODE_STRING* dest[]{&logon->Logon.LogonDomainName,&logon->Logon.UserName,&logon->Logon.Password};size_t offset=sizeof(*logon);
 for(int i=0;i<3;i++){
  size_t n=fields[i]->size()*sizeof(wchar_t);if(n>static_cast<size_t>(USHRT_MAX)){SecureZeroMemory(buffer,bytes);CoTaskMemFree(buffer);return E_INVALIDARG;}
  dest[i]->Length=dest[i]->MaximumLength=static_cast<USHORT>(n);dest[i]->Buffer=reinterpret_cast<PWSTR>(offset);memcpy(buffer+offset,fields[i]->data(),n);offset+=n;
 }
 out->ulAuthenticationPackage=auth;out->clsidCredentialProvider=CLSID_Linad;out->cbSerialization=static_cast<ULONG>(bytes);out->rgbSerialization=buffer;return S_OK;
}
class Credential final:public ICredentialProviderCredential2 {
 std::atomic<ULONG> refs{1};ICredentialProviderCredentialEvents* events=nullptr;
 std::wstring user,password;linad::Grant pending;
public:
 CREDENTIAL_PROVIDER_USAGE_SCENARIO usage=CPUS_LOGON;
 Credential(){objects++;}~Credential(){if(events)events->Release();linad::wipe(password);objects--;}
 HRESULT STDMETHODCALLTYPE QueryInterface(REFIID iid,void** out)override{if(!out)return E_POINTER;*out=nullptr;if(iid==IID_IUnknown||iid==IID_ICredentialProviderCredential||iid==IID_ICredentialProviderCredential2){*out=static_cast<ICredentialProviderCredential2*>(this);AddRef();return S_OK;}return E_NOINTERFACE;}
 ULONG STDMETHODCALLTYPE AddRef()override{return ++refs;}ULONG STDMETHODCALLTYPE Release()override{auto n=--refs;if(!n)delete this;return n;}
 HRESULT STDMETHODCALLTYPE Advise(ICredentialProviderCredentialEvents* e)override{if(events)events->Release();events=e;if(events)events->AddRef();return S_OK;}
 HRESULT STDMETHODCALLTYPE UnAdvise()override{if(events){events->Release();events=nullptr;}return S_OK;}
 HRESULT STDMETHODCALLTYPE SetSelected(BOOL* autoLogon)override{if(!autoLogon)return E_POINTER;*autoLogon=FALSE;return S_OK;}
 HRESULT STDMETHODCALLTYPE SetDeselected()override{linad::wipe(password);if(events)events->SetFieldString(this,2,L"");return S_OK;}
 HRESULT STDMETHODCALLTYPE GetFieldState(DWORD field,CREDENTIAL_PROVIDER_FIELD_STATE* state,CREDENTIAL_PROVIDER_FIELD_INTERACTIVE_STATE* interactive)override{if(field>=5||!state||!interactive)return E_INVALIDARG;*state=field==0?CPFS_DISPLAY_IN_BOTH:CPFS_DISPLAY_IN_SELECTED_TILE;*interactive=field==1?CPFIS_FOCUSED:CPFIS_NONE;return S_OK;}
 HRESULT STDMETHODCALLTYPE GetStringValue(DWORD field,PWSTR* value)override{switch(field){case 0:return duplicate(L"Linad",value);case 1:return duplicate(user.c_str(),value);case 2:return duplicate(password.c_str(),value);case 3:return duplicate(L"Anmelden",value);case 4:return duplicate(L"Linad-Konto · Online-Anmeldung",value);default:return E_INVALIDARG;}}
 HRESULT STDMETHODCALLTYPE GetBitmapValue(DWORD,HBITMAP*)override{return E_NOTIMPL;}
 HRESULT STDMETHODCALLTYPE GetCheckboxValue(DWORD,BOOL*,PWSTR*)override{return E_NOTIMPL;}
 HRESULT STDMETHODCALLTYPE GetSubmitButtonValue(DWORD field,DWORD* adjacent)override{if(field!=3||!adjacent)return E_INVALIDARG;*adjacent=2;return S_OK;}
 HRESULT STDMETHODCALLTYPE GetComboBoxValueCount(DWORD,DWORD*,DWORD*)override{return E_NOTIMPL;}
 HRESULT STDMETHODCALLTYPE GetComboBoxValueAt(DWORD,DWORD,PWSTR*)override{return E_NOTIMPL;}
 HRESULT STDMETHODCALLTYPE SetStringValue(DWORD field,PCWSTR value)override{if(!value)return E_INVALIDARG;if(field==1){if(wcslen(value)>64)return E_INVALIDARG;user=value;return S_OK;}if(field==2){linad::wipe(password);if(wcslen(value)>512)return E_INVALIDARG;password=value;return S_OK;}return E_INVALIDARG;}
 HRESULT STDMETHODCALLTYPE SetCheckboxValue(DWORD,BOOL)override{return E_NOTIMPL;}
 HRESULT STDMETHODCALLTYPE SetComboBoxSelectedValue(DWORD,DWORD)override{return E_NOTIMPL;}
 HRESULT STDMETHODCALLTYPE CommandLinkClicked(DWORD)override{return E_NOTIMPL;}
 HRESULT STDMETHODCALLTYPE GetSerialization(CREDENTIAL_PROVIDER_GET_SERIALIZATION_RESPONSE* response,CREDENTIAL_PROVIDER_CREDENTIAL_SERIALIZATION* serialization,PWSTR* text,CREDENTIAL_PROVIDER_STATUS_ICON* icon)override{
  if(!response||!serialization||!text||!icon)return E_POINTER;
  *response=CPGSR_NO_CREDENTIAL_NOT_FINISHED;ZeroMemory(serialization,sizeof(*serialization));*text=nullptr;*icon=CPSI_ERROR;
  linad::complete(pending);
  bool authenticated=linad::broker(1,user,password,pending);linad::wipe(password);if(events)events->SetFieldString(this,2,L"");
  if(!authenticated){linad::wipe(pending.password);linad::wipe(pending.ticket);duplicate(L"Anmeldung nicht möglich. Passwort, Gerätefreigabe und Serververbindung prüfen.",text);return S_OK;}
  HRESULT hr=serialize(pending,usage,serialization);linad::wipe(pending.password);
  if(FAILED(hr)){linad::complete(pending);duplicate(L"Windows-Anmeldung konnte nicht vorbereitet werden.",text);return S_OK;}
  *icon=CPSI_NONE;*response=CPGSR_RETURN_CREDENTIAL_FINISHED;return S_OK;
 }
 HRESULT STDMETHODCALLTYPE ReportResult(NTSTATUS status,NTSTATUS,PWSTR* text,CREDENTIAL_PROVIDER_STATUS_ICON* icon)override{if(text)*text=nullptr;if(icon)*icon=status?CPSI_ERROR:CPSI_NONE;linad::complete(pending);return S_OK;}
 HRESULT STDMETHODCALLTYPE GetUserSid(PWSTR* sid)override{if(!sid)return E_POINTER;*sid=nullptr;return S_FALSE;}
};
class Provider final:public ICredentialProvider,public ICredentialProviderSetUserArray {
 std::atomic<ULONG> refs{1};Credential* credential=new(std::nothrow)Credential;
public:
 Provider(){objects++;}~Provider(){if(credential)credential->Release();objects--;}
 HRESULT STDMETHODCALLTYPE QueryInterface(REFIID iid,void** out)override{if(!out)return E_POINTER;*out=nullptr;if(iid==IID_IUnknown||iid==IID_ICredentialProvider)*out=static_cast<ICredentialProvider*>(this);else if(iid==IID_ICredentialProviderSetUserArray)*out=static_cast<ICredentialProviderSetUserArray*>(this);else return E_NOINTERFACE;AddRef();return S_OK;}
 ULONG STDMETHODCALLTYPE AddRef()override{return ++refs;}ULONG STDMETHODCALLTYPE Release()override{auto n=--refs;if(!n)delete this;return n;}
 HRESULT STDMETHODCALLTYPE SetUsageScenario(CREDENTIAL_PROVIDER_USAGE_SCENARIO usage,DWORD)override{if(usage!=CPUS_LOGON&&usage!=CPUS_UNLOCK_WORKSTATION)return E_NOTIMPL;if(!credential)return E_OUTOFMEMORY;credential->usage=usage;return S_OK;}
 HRESULT STDMETHODCALLTYPE SetSerialization(const CREDENTIAL_PROVIDER_CREDENTIAL_SERIALIZATION*)override{return E_NOTIMPL;}
 HRESULT STDMETHODCALLTYPE Advise(ICredentialProviderEvents*,UINT_PTR)override{return S_OK;}
 HRESULT STDMETHODCALLTYPE UnAdvise()override{return S_OK;}
 HRESULT STDMETHODCALLTYPE GetFieldDescriptorCount(DWORD* count)override{if(!count)return E_POINTER;*count=5;return S_OK;}
 HRESULT STDMETHODCALLTYPE GetFieldDescriptorAt(DWORD field,CREDENTIAL_PROVIDER_FIELD_DESCRIPTOR** out)override{
  if(field>=5||!out)return E_INVALIDARG;
  *out=nullptr;const CREDENTIAL_PROVIDER_FIELD_TYPE types[]{CPFT_LARGE_TEXT,CPFT_EDIT_TEXT,CPFT_PASSWORD_TEXT,CPFT_SUBMIT_BUTTON,CPFT_SMALL_TEXT};const wchar_t* labels[]{L"Linad",L"Linad-Benutzername",L"Linad-Passwort",L"Anmelden",L"Online-Anmeldung"};
  auto d=static_cast<CREDENTIAL_PROVIDER_FIELD_DESCRIPTOR*>(CoTaskMemAlloc(sizeof(CREDENTIAL_PROVIDER_FIELD_DESCRIPTOR)));if(!d)return E_OUTOFMEMORY;ZeroMemory(d,sizeof(*d));d->dwFieldID=field;d->cpft=types[field];HRESULT hr=duplicate(labels[field],&d->pszLabel);if(FAILED(hr)){CoTaskMemFree(d);return hr;}*out=d;return S_OK;
 }
 HRESULT STDMETHODCALLTYPE GetCredentialCount(DWORD* count,DWORD* selected,BOOL* autoLogon)override{if(!count||!selected||!autoLogon)return E_POINTER;*count=1;*selected=0;*autoLogon=FALSE;return S_OK;}
 HRESULT STDMETHODCALLTYPE GetCredentialAt(DWORD index,ICredentialProviderCredential** out)override{if(index||!out)return E_INVALIDARG;if(!credential)return E_OUTOFMEMORY;return credential->QueryInterface(IID_ICredentialProviderCredential,reinterpret_cast<void**>(out));}
 HRESULT STDMETHODCALLTYPE SetUserArray(ICredentialProviderUserArray*)override{return S_OK;}
};
class Factory final:public IClassFactory{
 std::atomic<ULONG> refs{1};
public:
 Factory(){objects++;}~Factory(){objects--;}
 HRESULT STDMETHODCALLTYPE QueryInterface(REFIID iid,void** out)override{if(!out)return E_POINTER;*out=nullptr;if(iid!=IID_IUnknown&&iid!=IID_IClassFactory)return E_NOINTERFACE;*out=this;AddRef();return S_OK;}
 ULONG STDMETHODCALLTYPE AddRef()override{return ++refs;}ULONG STDMETHODCALLTYPE Release()override{auto n=--refs;if(!n)delete this;return n;}
 HRESULT STDMETHODCALLTYPE CreateInstance(IUnknown* outer,REFIID iid,void** out)override{if(outer)return CLASS_E_NOAGGREGATION;auto p=new(std::nothrow)Provider;if(!p)return E_OUTOFMEMORY;HRESULT hr=p->QueryInterface(iid,out);p->Release();return hr;}
 HRESULT STDMETHODCALLTYPE LockServer(BOOL lock)override{if(lock)objects++;else objects--;return S_OK;}
};
extern "C" __declspec(dllexport) HRESULT __stdcall DllGetClassObject(REFCLSID clsid,REFIID iid,void** out){if(clsid!=CLSID_Linad)return CLASS_E_CLASSNOTAVAILABLE;auto factory=new(std::nothrow)Factory;if(!factory)return E_OUTOFMEMORY;HRESULT hr=factory->QueryInterface(iid,out);factory->Release();return hr;}
extern "C" __declspec(dllexport) HRESULT __stdcall DllCanUnloadNow(){return objects==0?S_OK:S_FALSE;}
BOOL WINAPI DllMain(HINSTANCE,DWORD,LPVOID){return TRUE;}
