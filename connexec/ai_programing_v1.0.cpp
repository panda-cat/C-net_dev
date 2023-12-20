#include <iostream>
#include <vector>
#include <future>
#include <thread>
#include <mutex>
#include <fstream>
#include <chrono>
#include <netmiko.hpp>

using namespace std;
using namespace netmiko;

mutex mtx;

void load_excel(string excel_file, vector<map<string, string>>& devices) {
  ifstream file(excel_file);
  if (!file.is_open()) {
    cerr << "Error: Unable to open Excel file" << endl;
    return;
  }

  string line;
  getline(file, line); // Skip header
  while (getline(file, line)) {
    map<string, string> device;
    stringstream ss(line);
    string field;

    getline(ss, field, ',');
    device["host"] = field;
    getline(ss, field, ',');
    device["username"] = field;
    getline(ss, field, ',');
    device["device_type"] = field;
    getline(ss, field, ',');
    device["password"] = field;
    getline(ss, field, ',');
    device["secret"] = field;
    getline(ss, field, ',');
    device["readtime"] = field;
    getline(ss, field, ',');
    device["mult_command"] = field;

    devices.push_back(device);
  }

  file.close();
}

void execute_commands(const map<string, string>& device) {
  string ip = device["host"];
  string user = device["username"];
  string dev_type = device["device_type"];
  string passwd = device["password"];
  string secret = device["secret"];
  string read_time = device["readtime"];
  vector<string> cmds = split(device["mult_command"], ';');

  try {
    ConnectOptions conn_opts;
    conn_opts.username = user;
    conn_opts.password = passwd;
    conn_opts.secret = secret;
    conn_opts.read_timeout_override = stoi(read_time);

    BaseConnection conn(dev_type, ip, conn_opts);
    conn.connect();

    string output;
    if (dev_type == " PaloAltoPanorama") {
      output = conn.send_multiline(cmds, "> ");
    } else if (dev_type == "Huawei" || dev_type == "HuaweiTelnet"
        || dev_type == "HPComware" || dev_type == "HPComwareTelnet") {
      output = conn.send_multiline(cmds);
    } else {
      conn.enable();
      output = conn.send_multiline(cmds);
    }

    conn.disconnect();

    mtx.lock();
    string output_dir = "./result" + get_current_date_str();
    create_directory(output_dir);
    ofstream file(output_dir + "/" + ip + ".txt");
    file << output;
    file.close();
    mtx.unlock();

    cout << "Executed commands on " << ip << endl;
  } catch (const NetmikoTimeoutException& e) {
    mtx.lock();
    ofstream file("login_failed_list.txt", ios::app);
    file << ip << " Login timed out" << endl;
    file.close();
    mtx.unlock();
    cout << "Login timed out on " << ip << endl;
  } catch (const NetmikoAuthenticationException& e) {
    mtx.lock();
    ofstream file("login_failed_list.txt", ios::app);
    file << ip << " Invalid username or password" << endl;
    file.close();
    mtx.unlock();
    cout << "Invalid username or password on " << ip << endl;
  }
}

int main(int argc, char* argv[]) {
  string excel_file;
  int num_threads = 4;

  try {
    int opt;
    while ((opt = getopt(argc, argv, "c:t:")) != -1) {
      switch (opt) {
        case 'c':
          excel_file = optarg;
          break;
        case 't':
          num_threads = stoi(optarg);
          break;
        default:
          cout << "Usage: connexec -c <excel_file> -t <num_threads default:4>" << endl;
          return 1;
      }
    }
  } catch (exception& e) {
    cout << "Error: " << e.what() << endl;
    cout << "Usage: connexec -c <excel_file> -t <num_threads default:4>" << endl;
    return 1;
  }

  if (excel_file.empty()) {
    cout << "Error: Excel file not specified" << endl;
    cout << "Usage: connexec -c <excel_file> -t <num_threads default:4>" << endl;
    return 1;
  }

  vector<map<string, string>> devices;
  load_excel(excel_file, devices);

  vector<future<void>> futures;
  for (const auto& device : devices) {
    futures.push_back(async(launch::async, execute_commands, device));
  }
  for (auto& f : futures) {
    f.wait();
  }

  return 0;
}
