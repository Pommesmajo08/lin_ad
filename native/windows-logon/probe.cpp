#define SECURITY_WIN32
#include "broker.h"
#ifdef __MINGW32__
#include "credentialprovider_compat.h"
#else
#include <credentialprovider.h>
#endif
#include <ntsecapi.h>
#include <userenv.h>
#include <fstream>
#include <iostream>
#include <iterator>

static const GUID CLSID_Linad={0x1d066a31,0x71d4,0x45f4,{0x91,0x28,0x37,0xa3,0x6b,0x1a,0x42,0x56}};
using GetClass=HRESULT (WINAPI*)(REFCLSID,REFIID,void**);
using CanUnload=HRESULT (WINAPI*)();
static bool privilege(const wchar_t* name){HANDLE token=nullptr;if(!OpenProcessToken(GetCurrentProcess(),TOKEN_ADJUST_PRIVILEGES|TOKEN_QUERY,&token))return false;TOKEN_PRIVILEGES p{};p.PrivilegeCount=1;p.Privileges[0].Attributes=SE_PRIVILEGE_ENABLED;bool ok=LookupPrivilegeValueW(nullptr,name,&p.Privileges[0].Luid)!=FALSE;if(ok){SetLastError(ERROR_SUCCESS);ok=AdjustTokenPrivileges(token,FALSE,&p,0,nullptr,nullptr)!=FALSE&&GetLastError()==ERROR_SUCCESS;}CloseHandle(token);return ok;}
static std::wstring unpack(const UNICODE_STRING& field,const CREDENTIAL_PROVIDER_CREDENTIAL_SERIALIZATION& serial){auto offset=reinterpret_cast<uintptr_t>(field.Buffer);if(offset>serial.cbSerialization||field.Length>serial.cbSerialization-offset||field.Length%sizeof(wchar_t))return {};return {reinterpret_cast<wchar_t*>(serial.rgbSerialization+offset),field.Length/sizeof(wchar_t)};}
int wmain(int argc,wchar_t** argv){
 if(argc<2||argc>3)return 2;
 HMODULE dll=LoadLibraryW(argv[1]);if(!dll){std::cerr<<"DLL load failed "<<GetLastError()<<"\n";return 3;}
 GetClass getClass=nullptr;CanUnload canUnload=nullptr;FARPROC first=GetProcAddress(dll,"DllGetClassObject"),second=GetProcAddress(dll,"DllCanUnloadNow");static_assert(sizeof(first)==sizeof(getClass));memcpy(&getClass,&first,sizeof(getClass));memcpy(&canUnload,&second,sizeof(canUnload));if(!getClass||!canUnload)return 4;
 IClassFactory* factory=nullptr;ICredentialProvider* provider=nullptr;ICredentialProviderCredential* credential=nullptr;
 if(FAILED(getClass(CLSID_Linad,IID_IClassFactory,reinterpret_cast<void**>(&factory))))return 5;
 HRESULT hr=factory->CreateInstance(nullptr,IID_ICredentialProvider,reinterpret_cast<void**>(&provider));factory->Release();if(FAILED(hr))return 6;
 if(FAILED(provider->SetUsageScenario(CPUS_LOGON,0)))return 7;
 DWORD count=0;if(FAILED(provider->GetFieldDescriptorCount(&count))||count!=5)return 8;
 for(DWORD i=0;i<count;i++){CREDENTIAL_PROVIDER_FIELD_DESCRIPTOR* field=nullptr;if(FAILED(provider->GetFieldDescriptorAt(i,&field))||field->dwFieldID!=i)return 9;CoTaskMemFree(field->pszLabel);CoTaskMemFree(field);}
 if(FAILED(provider->GetCredentialAt(0,&credential)))return 10;
 if(argc==3){
  HANDLE input=CreateFileW(argv[2],GENERIC_READ,FILE_SHARE_READ,nullptr,OPEN_EXISTING,0,nullptr);if(input==INVALID_HANDLE_VALUE)return 11;
  char data[1024]{};DWORD read=0;BOOL loaded=ReadFile(input,data,sizeof(data),&read,nullptr);CloseHandle(input);if(!loaded||read==sizeof(data))return 12;
  std::string source(data,read);SecureZeroMemory(data,sizeof(data));auto split=source.find('\n');if(split==std::string::npos)return 13;
  std::wstring name,password;if(!linad::wide(source.substr(0,split),name)||!linad::wide(source.substr(split+1),password))return 14;linad::wipe(source);
  credential->SetStringValue(1,name.c_str());credential->SetStringValue(2,password.c_str());linad::wipe(password);
  CREDENTIAL_PROVIDER_GET_SERIALIZATION_RESPONSE response{};CREDENTIAL_PROVIDER_CREDENTIAL_SERIALIZATION serial{};PWSTR text=nullptr;CREDENTIAL_PROVIDER_STATUS_ICON icon{};
  hr=credential->GetSerialization(&response,&serial,&text,&icon);if(text)CoTaskMemFree(text);
  if(FAILED(hr)||response!=CPGSR_RETURN_CREDENTIAL_FINISHED||serial.cbSerialization<sizeof(KERB_INTERACTIVE_UNLOCK_LOGON))return 15;
  auto logon=reinterpret_cast<KERB_INTERACTIVE_UNLOCK_LOGON*>(serial.rgbSerialization);
  auto login=unpack(logon->Logon.UserName,serial),domain=unpack(logon->Logon.LogonDomainName,serial),secret=unpack(logon->Logon.Password,serial);
  if(login.empty()||domain.empty()||secret.empty())return 16;
  HANDLE token=nullptr;
  if(!LogonUserW(login.c_str(),domain.c_str(),secret.c_str(),LOGON32_LOGON_INTERACTIVE,LOGON32_PROVIDER_DEFAULT,&token)){std::cerr<<"SAM authentication failed "<<GetLastError()<<"\n";return 17;}
  SecureZeroMemory(serial.rgbSerialization,serial.cbSerialization);CoTaskMemFree(serial.rgbSerialization);
  credential->ReportResult(0,0,&text,&icon);if(text)CoTaskMemFree(text);
  HANDLE second=nullptr;if(LogonUserW(login.c_str(),domain.c_str(),secret.c_str(),LOGON32_LOGON_INTERACTIVE,LOGON32_PROVIDER_DEFAULT,&second)){CloseHandle(second);return 18;}linad::wipe(secret);
  if(!privilege(SE_BACKUP_NAME)||!privilege(SE_RESTORE_NAME))return 19;
  PROFILEINFOW profile{};profile.dwSize=sizeof(profile);profile.dwFlags=PI_NOUI;profile.lpUserName=login.data();
  if(!LoadUserProfileW(token,&profile)){std::cerr<<"Profile load failed "<<GetLastError()<<"\n";return 20;}
  if(!UnloadUserProfile(token,profile.hProfile))return 21;
  CloseHandle(token);
 }
 credential->Release();provider->Release();if(canUnload()!=S_OK)return 22;FreeLibrary(dll);
 std::cout<<"PASS: credential provider COM/serialization, online broker, local SAM and profile (when credentials supplied)\n";return 0;
}
