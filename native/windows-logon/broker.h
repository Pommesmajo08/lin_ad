#pragma once
#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include <string>
#include <vector>
#include <cstdint>

namespace linad {
inline void wipe(std::wstring& text) { if(!text.empty()) SecureZeroMemory(text.data(),text.size()*sizeof(wchar_t));text.clear(); }
inline void wipe(std::string& text) { if(!text.empty()) SecureZeroMemory(text.data(),text.size());text.clear(); }
struct Grant {
 std::wstring login,password,ticket;
 ~Grant(){wipe(password);wipe(ticket);}
};
inline std::string utf8(const std::wstring& text) {
 if(text.empty())return {};
 int n=WideCharToMultiByte(CP_UTF8,WC_ERR_INVALID_CHARS,text.data(),static_cast<int>(text.size()),nullptr,0,nullptr,nullptr);
 if(!n)return {};
 std::string out(n,'\0');WideCharToMultiByte(CP_UTF8,WC_ERR_INVALID_CHARS,text.data(),static_cast<int>(text.size()),out.data(),n,nullptr,nullptr);return out;
}
inline bool wide(const std::string& text,std::wstring& out){
 if(text.empty()){out.clear();return true;}
 int n=MultiByteToWideChar(CP_UTF8,MB_ERR_INVALID_CHARS,text.data(),static_cast<int>(text.size()),nullptr,0);if(!n)return false;
 out.resize(n);return MultiByteToWideChar(CP_UTF8,MB_ERR_INVALID_CHARS,text.data(),static_cast<int>(text.size()),out.data(),n)==n;
}
inline bool systemServer(HANDLE pipe) {
 ULONG pid=0;if(!GetNamedPipeServerProcessId(pipe,&pid))return false;
 HANDLE process=OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION,FALSE,pid);if(!process)return false;
 HANDLE token=nullptr;bool valid=false;
 if(OpenProcessToken(process,TOKEN_QUERY,&token)){
  DWORD needed=0;GetTokenInformation(token,TokenUser,nullptr,0,&needed);std::vector<BYTE> buf(needed);
  if(needed&&GetTokenInformation(token,TokenUser,buf.data(),needed,&needed))valid=IsWellKnownSid(reinterpret_cast<TOKEN_USER*>(buf.data())->User.Sid,WinLocalSystemSid)!=FALSE;
  CloseHandle(token);
 }CloseHandle(process);return valid;
}
inline bool transfer(HANDLE pipe,void* buffer,DWORD size,bool write,DWORD* failure=nullptr) {
 BYTE* data=static_cast<BYTE*>(buffer);
 while(size){
  OVERLAPPED ov{};ov.hEvent=CreateEventW(nullptr,TRUE,FALSE,nullptr);if(!ov.hEvent)return false;
  DWORD n=0;BOOL ok=write?WriteFile(pipe,data,size,&n,&ov):ReadFile(pipe,data,size,&n,&ov);
  DWORD error=ok?ERROR_SUCCESS:GetLastError();
  if(!ok&&error==ERROR_IO_PENDING){
   if(WaitForSingleObject(ov.hEvent,25000)!=WAIT_OBJECT_0){CancelIoEx(pipe,&ov);WaitForSingleObject(ov.hEvent,INFINITE);CloseHandle(ov.hEvent);if(failure)*failure=WAIT_TIMEOUT;return false;}
   ok=GetOverlappedResult(pipe,&ov,&n,FALSE);error=ok?ERROR_SUCCESS:GetLastError();
  }
  CloseHandle(ov.hEvent);if(!ok||!n){if(failure)*failure=error;return false;}data+=n;size-=n;
 }return true;
}
inline bool broker(DWORD operation,const std::wstring& first,const std::wstring& second,Grant& result){
 constexpr const wchar_t* name=L"\\\\.\\pipe\\LinadLogon-v1";
 if(!WaitNamedPipeW(name,3000))return false;
 HANDLE pipe=CreateFileW(name,GENERIC_READ|GENERIC_WRITE,0,nullptr,OPEN_EXISTING,FILE_FLAG_OVERLAPPED,nullptr);
 if(pipe==INVALID_HANDLE_VALUE)return false;
 // Check SYSTEM identity before disclosing the directory password to the pipe.
 if(!systemServer(pipe)){CloseHandle(pipe);return false;}
 std::string user=utf8(first),secret=utf8(second);
 if(user.empty()||user.size()>64||secret.size()>512){wipe(secret);CloseHandle(pipe);return false;}
 DWORD header[4]{0x4c414431,operation,static_cast<DWORD>(user.size()),static_cast<DWORD>(secret.size())};
 bool ok=transfer(pipe,header,sizeof(header),true)&&transfer(pipe,user.data(),static_cast<DWORD>(user.size()),true)&&transfer(pipe,secret.data(),static_cast<DWORD>(secret.size()),true);
 wipe(secret);DWORD status=1;if(ok)ok=transfer(pipe,&status,sizeof(status),false);
 for(std::wstring* field:{&result.login,&result.password,&result.ticket}){
  if(!ok)break;
  DWORD length=0;ok=transfer(pipe,&length,sizeof(length),false)&&length<=4096;
  std::string bytes(ok?length:0,'\0');if(ok&&length)ok=transfer(pipe,bytes.data(),length,false);if(ok)ok=wide(bytes,*field);wipe(bytes);
 }
 if(ok){BYTE ack=0x7f;ok=transfer(pipe,&ack,1,true);if(ok){BYTE end=0;DWORD error=0;bool extra=transfer(pipe,&end,1,false,&error);ok=!extra&&(error==ERROR_BROKEN_PIPE||error==ERROR_PIPE_NOT_CONNECTED||error==ERROR_NO_DATA);}}
 CloseHandle(pipe);return ok&&status==0;
}
inline void complete(Grant& grant){if(!grant.login.empty()&&!grant.ticket.empty()){Grant ignored;broker(2,grant.login,grant.ticket,ignored);}wipe(grant.password);wipe(grant.ticket);grant.login.clear();}
}
