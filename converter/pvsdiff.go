package main

import "fmt"

// cmdPVSDiff compares the texture tables of two pvs files field by field.
func cmdPVSDiff(a, b string) {
	_, ra, err := readPVS(a)
	if err != nil {
		fmt.Println(err)
		return
	}
	_, rb, err := readPVS(b)
	if err != nil {
		fmt.Println(err)
		return
	}
	if len(ra) != len(rb) {
		fmt.Println("record counts differ", len(ra), len(rb))
		return
	}
	dt, dg, df := 0, 0, 0
	shown := 0
	for i := range ra {
		x, y := ra[i], rb[i]
		diff := false
		if x.total != y.total {
			dt++
			diff = true
		}
		if x.flags != y.flags {
			df++
		}
		for g := range x.groups {
			if x.groups[g] != y.groups[g] {
				dg++
				diff = true
				break
			}
		}
		if diff && shown < 8 {
			shown++
			fmt.Printf("rec %d %dx%d mips %d flags %08x/%08x total %x/%x groups %x / %x\n", i, x.w, x.h, x.mips, x.flags, y.flags, x.total, y.total, x.groups, y.groups)
		}
	}
	fmt.Printf("%d records: total differs %d, groups differ %d, flags differ %d\n", len(ra), dt, dg, df)
	for i := 0; i < 3; i++ {
		x := ra[i]
		fmt.Printf("orig rec %d %dx%d mips %d flags %08x total %x groups %x\n", i, x.w, x.h, x.mips, x.flags, x.total, x.groups)
	}
}
